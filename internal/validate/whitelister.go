package validate

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	cache "github.com/patrickmn/go-cache"
)

type WhitelisterApi struct {
	CheckIpApiUrl     string        `yaml:"check_ip_api_url"`
	CheckDomainApiUrl string        `yaml:"check_domain_api_url"`
	MaxTries          int           `yaml:"max_tries"`
	SleepTime         time.Duration `yaml:"sleep_time"`
}

type IpWhiteListResponse struct {
	Status string `json:"status"`
	IP     string `json:"ip"`
	Result bool   `json:"result"`
}

type DomainWhiteListResponse struct {
	Status string `json:"status"`
	Domain string `json:"domain"`
	Result bool   `json:"result"`
}

type Whitelister struct {
	sync.Mutex
	checkDomainApiUrl string
	checkIpApiUrl     string
	maxTries          int
	sleepTime         time.Duration
	memcache          *cache.Cache
}

func NewWhitelister(cfg WhitelisterApi) *Whitelister {
	wl := &Whitelister{
		checkDomainApiUrl: cfg.CheckDomainApiUrl,
		checkIpApiUrl:     cfg.CheckIpApiUrl,
		maxTries:          cfg.MaxTries,
		sleepTime:         cfg.SleepTime,
		memcache:          cache.New(time.Hour, time.Minute),
	}
	return wl
}

func (checker *Whitelister) DomainIsWhite(domain string) (bool, error) {
	checker.Lock()
	defer checker.Unlock()

	var msg string
	var isWhite bool
	fnc := "wl check domain"
	maxTries := checker.maxTries
	url := fmt.Sprintf(checker.checkDomainApiUrl, domain)

	if net.ParseIP(domain) != nil {
		return false, nil
	}
	isWhiteItf, cached := checker.memcache.Get(domain)
	if cached {
		return isWhiteItf.(bool), nil
	}

	for try := 1; try <= maxTries; try++ {

		if try > 1 {
			// mt.IncVec(mt.Errors, fnc)
			sleepDuration := checker.sleepTime * time.Duration(try)
			if sleepDuration > 0 {
				log.Printf("%v (%v / sleep for %v)", fnc, try, sleepDuration)
				time.Sleep(sleepDuration)
			}
		}

		resp, err := http.Get(url)
		if err != nil {
			msg = fmt.Sprintf("%v (%v / can't execute request), domain: %v, err: %v",
				fnc, try, domain, err)
			log.Print(msg)
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			msg = fmt.Sprintf("%v (%v / can't read response body), domain: %v, status: %v, err: %v",
				fnc, try, domain, resp.StatusCode, err)
			log.Print(msg)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			msg = fmt.Sprintf("%v (%v / status = %v), domain: %v, err: %v",
				fnc, try, domain, resp.StatusCode, err)
			log.Print(msg)
			continue
		}

		var response DomainWhiteListResponse
		if err := json.Unmarshal(body, &response); err != nil {
			msg = fmt.Sprintf("%v (%v / can't parse json from response), domain: %v, status: %v, body: %v, err: %v",
				fnc, try, domain, resp.StatusCode, TrimBytes(body), err)
			log.Print(msg)
			continue
		}

		isWhite = response.Result
		checker.memcache.Set(domain, isWhite, cache.DefaultExpiration)
		return isWhite, nil
	}

	msg = fmt.Sprintf("%v - no result after %d tries, error: %v", fnc, maxTries, msg)
	log.Print(msg)
	// mt.IncVec(mt.CapturedFatalsErrors, fnc)
	return false, nil
}

func (checker *Whitelister) IpIsWhite(ip string) (bool, error) {
	checker.Lock()
	defer checker.Unlock()

	var msg string
	var isWhite bool
	fnc := "wl check ip"
	maxTries := checker.maxTries
	url := fmt.Sprintf(checker.checkIpApiUrl, ip)

	isWhiteItf, cached := checker.memcache.Get(ip)
	if cached {
		return isWhiteItf.(bool), nil
	}

	for try := 1; try <= maxTries; try++ {

		if try > 1 {
			// mt.IncVec(mt.Errors, fnc)
			sleepDuration := checker.sleepTime * time.Duration(try)
			if sleepDuration > 0 {
				log.Printf("%v (%v / sleep for %v)", fnc, try, sleepDuration)
				time.Sleep(sleepDuration)
			}
		}

		resp, err := http.Get(url)
		if err != nil {
			msg = fmt.Sprintf("%v (%v / can't execute request), ip: %v, err: %v",
				fnc, try, ip, err)
			log.Print(msg)
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			msg = fmt.Sprintf("%v (%v / can't read response body), ip: %v, status: %v, err: %v",
				fnc, try, ip, resp.StatusCode, err)
			log.Print(msg)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			msg = fmt.Sprintf("%v (%v / status = %v), ip: %v",
				fnc, try, resp.StatusCode, ip)
			log.Print(msg)
			continue
		}

		var response IpWhiteListResponse
		if err := json.Unmarshal(body, &response); err != nil {
			msg = fmt.Sprintf("%v (%v / can't parse json from response), ip: %v, status: %v, body: %v, err: %v",
				fnc, try, ip, resp.StatusCode, TrimBytes(body), err)
			log.Print(msg)
			continue
		}

		isWhite = response.Result
		checker.memcache.Set(ip, isWhite, cache.DefaultExpiration)
		return isWhite, nil
	}

	msg = fmt.Sprintf("%v - no result after %d tries, error: %v", fnc, maxTries, msg)
	log.Print(msg)
	// mt.IncVec(mt.CapturedFatalsErrors, fnc)
	return false, nil
}
