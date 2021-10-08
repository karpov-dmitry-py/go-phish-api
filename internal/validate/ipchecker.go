package validate

import (
	"errors"
	"log"
	"net"
)

type IpChecker struct {
	LocalIPNets []*net.IPNet
}

func NewIpChecker(localNets []string) *IpChecker {
	var nets []*net.IPNet
	checker := &IpChecker{}
	for _, localNet := range localNets {
		_, net, err := net.ParseCIDR(localNet)
		if err != nil {
			log.Fatalf("ip checker init error (parse local ip nets error) %v: %v", localNet, err)
		}
		nets = append(nets, net)
	}
	checker.LocalIPNets = nets
	return checker
}

func (checker *IpChecker) IsLocalIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	for _, net := range checker.LocalIPNets {
		if net.Contains(ip) {
			return true
		}
	}
	return false
}

func (checker *IpChecker) GetNetIP(domain string) net.IP {
	return net.ParseIP(domain)
}

func (checker *IpChecker) DomainIsIP(domain string) bool {
	return checker.GetNetIP(domain) != nil
}

func (checker *IpChecker) GetDomainIP(domain string) (string, error) {
	if checker.DomainIsIP(domain) {
		return domain, nil
	}

	ips, err := net.LookupHost(domain)
	if err != nil {
		log.Printf("get a-record fail (net.LookupHost() error):%v > %v", domain, err)
		return "", err
	}
	if len(ips) == 0 {
		log.Printf("get a-record fail (empty list received): %v", domain)
		return "", errors.New("empty list of a-records received")

	}
	ip := ips[0]
	log.Printf("get a-record ok: %v > %v", domain, ip)
	return ip, nil
}
