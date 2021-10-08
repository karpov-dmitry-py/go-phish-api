package validate

import (
	"regexp"
)

type UrlBlacklister struct {
	Regexps []*regexp.Regexp
}

func NewBlacklister(rawRegexps []string) *UrlBlacklister {
	checker := &UrlBlacklister{}
	var compiled []*regexp.Regexp
	for _, exp := range rawRegexps {
		compiled = append(compiled, regexp.MustCompile(exp))
	}
	checker.Regexps = compiled
	return checker
}

func (checker *UrlBlacklister) UrlIsBlack(url string) bool {
	for _, re := range checker.Regexps {
		if re.MatchString(url) {
			return true
		}
	}
	return false
}
