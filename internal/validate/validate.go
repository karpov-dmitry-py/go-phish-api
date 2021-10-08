package validate

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

type ValidatorConfig struct {
	UrlBlackListRegexps []string       `yaml:"url_blacklist_regexps"`
	LocalIPNets         []string       `yaml:"local_ip_nets"`
	WhitelisterApi      WhitelisterApi `yaml:"whitelister_api"`
}

func (cfg *ValidatorConfig) IsValid() bool {
	valid := true
	action := "[validator cfg validation]"

	if cfg == nil {
		log.Printf("%v cfg is nil", action)
		return false
	}

	// bl regexps
	part := "bl regexps"
	blRegexps := cfg.UrlBlackListRegexps
	if len(blRegexps) == 0 {
		valid = false
		log.Printf("%v %v list is empty", action, part)
	}

	for index, rx := range blRegexps {
		if rx == "" {
			valid = false
			log.Printf("%v %v item # %v is empty", action, part, index+1)
		}
	}

	// ip checker - local ip nets
	part = "local ip nets"
	localIpNets := cfg.LocalIPNets
	if len(localIpNets) == 0 {
		valid = false
		log.Printf("%v %v list is empty", action, part)
	}

	for index, rx := range localIpNets {
		if rx == "" {
			valid = false
			log.Printf("%v %v item # %v is empty", action, part, index+1)
		}
	}

	// wl api
	part = "wl api"
	wlCfg := cfg.WhitelisterApi

	if !IsValidUrl(wlCfg.CheckDomainApiUrl) {
		valid = false
		log.Printf("%v %v domain check url is invalid", action, part)
	}

	if !IsValidUrl(wlCfg.CheckIpApiUrl) {
		valid = false
		log.Printf("%v %v ip check url is invalid", action, part)
	}

	if wlCfg.MaxTries <= 0 {
		valid = false
		log.Printf("%v %v retries count is invalid", action, part)
	}

	if wlCfg.SleepTime < time.Millisecond {
		valid = false
		log.Printf("%v %v sleep time is invalid", action, part)
	}

	return valid
}

type Validator struct {
	sync.Mutex
	DomainCache    *cache.Cache
	UrlBlacklister *UrlBlacklister
	IpChecker      *IpChecker
	Whitelister    *Whitelister
}

func NewValidator(cfg ValidatorConfig) (*Validator, error) {
	if !cfg.IsValid() {
		return nil, errors.New("validator cfg is invalid")
	}

	bl := NewBlacklister(cfg.UrlBlackListRegexps)
	ip := NewIpChecker(cfg.LocalIPNets)
	wl := NewWhitelister(cfg.WhitelisterApi)

	validator := &Validator{
		Mutex:          sync.Mutex{},
		DomainCache:    cache.New(30*time.Minute, 3*time.Minute),
		UrlBlacklister: bl,
		IpChecker:      ip,
		Whitelister:    wl,
	}
	return validator, nil
}

func (v *Validator) getDomainCache(domain string) (interface{}, bool) {
	v.Lock()
	defer v.Unlock()
	return v.DomainCache.Get(domain)
}

func (v *Validator) setDomainCache(domain string, val bool) {
	v.Lock()
	defer v.Unlock()
	v.DomainCache.SetDefault(domain, val)
}

func (v *Validator) UrlRequiresProcessing(url string) (bool, error) {

	if v.UrlBlacklister.UrlIsBlack(url) {
		log.Printf("url is blacklisted (does not need processing): %v", url)
		return false, nil
	}

	_, domain, err := v.ParseDomain(url)
	if err != nil {
		log.Printf("parse domain fail (%v): %v", url, err)
		return false, err
	}

	itf, isCached := v.getDomainCache(domain)
	if isCached {
		domainNeedsProcessing := itf.(bool)
		return domainNeedsProcessing, nil
	}

	result, err := v.DomainRequiresProcessing(domain)
	if err != nil {
		log.Printf("domain check fail (%v): %v >  %v", domain, url, err)
		return false, err
	}
	v.setDomainCache(domain, result)
	return result, nil
}

func (v *Validator) DomainIsWhiteListed(domain string) (bool, error) {
	if v.IpChecker.DomainIsIP(domain) {
		isWhite, err := v.Whitelister.IpIsWhite(domain)
		if err != nil {
			return false, err
		}
		return isWhite, nil
	} else {
		isWhite, err := v.Whitelister.DomainIsWhite(domain)
		if err != nil {
			return false, err
		}
		return isWhite, nil
	}
}

func (v *Validator) DomainHasARecord(domain string) bool {
	_, err := v.IpChecker.GetDomainIP(domain)
	if err != nil {
		log.Printf("domain has no a-record : %v", domain)
		return false
	}
	return true
}

func (v *Validator) DomainRequiresProcessing(domain string) (bool, error) {

	// domain is an ip address
	if v.IpChecker.DomainIsIP(domain) {
		netIP := v.IpChecker.GetNetIP(domain)
		if netIP == nil {
			log.Printf("domain has no a-record (does not need processing): %v", domain)
			return false, nil
		}

		if v.IpChecker.IsLocalIP(netIP) {
			log.Printf("domain is a local ip address (does not need processing): %v", domain)
			return false, nil
		}

		// check wl
		isWhite, err := v.Whitelister.IpIsWhite(domain)
		if err != nil {
			return false, err
		}
		if isWhite {
			log.Printf("ip is whitelisted (does not need processing): %v", domain)
		}
		return !isWhite, nil

		// domain is not an ip address
	} else {

		// check wl
		isWhite, err := v.Whitelister.DomainIsWhite(domain)
		if err != nil {
			return false, err
		}

		if isWhite {
			log.Printf("domain is whitelisted (does not need processing): %v", domain)
			return !isWhite, nil
		}

		// check a-record
		_, err = v.IpChecker.GetDomainIP(domain)
		if err != nil {
			log.Printf("domain has no a-record (does not need processing): %v", domain)
			return false, nil
		}
		return true, nil
	}
}

// ParseDomain returns full domain (domain with scheme), domain, error
func (v *Validator) ParseDomain(urlString string) (string, string, error) {

	if urlString == "" {
		return "", "", errors.New("received empty url to be parsed")
	}

	parsedData, err := url.Parse(urlString)
	if err != nil {
		return "", "", err
	}

	domain := parsedData.Hostname()
	if domain == "" {
		return "", "", errors.New("parsed empty domain from url")
	}

	if v.IpChecker.DomainIsIP(domain) {
		return v.getFullDomain(parsedData.Scheme, domain), domain, nil
	}

	return v.getFullDomain(parsedData.Scheme, domain), domain, nil
}

func (v *Validator) getFullDomain(scheme string, domain string) string {
	return fmt.Sprintf("%s://%s", scheme, domain)
}

func IsValidUrl(urlstr string) bool {
	var errMsg string
	u, err := url.Parse(urlstr)
	if err != nil {
		errMsg = fmt.Sprintf("url check (can't parse url): %v > %v", urlstr, err)
		log.Print(errMsg)
		return false
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		errMsg = fmt.Sprintf("url check (bad scheme - %v): %v", u.Scheme, urlstr)
		log.Print(errMsg)
		return false
	}
	return true
}
