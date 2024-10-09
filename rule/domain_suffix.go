package rules

import (
	"strings"

	C "github.com/yaling888/quirktiva/constant"
)

type DomainSuffix struct {
	*Base
	suffix  string
	adapter string
}

func (ds *DomainSuffix) RuleType() C.RuleType {
	return C.DomainSuffix
}

func (ds *DomainSuffix) Match(metadata *C.Metadata) bool {
	domain := metadata.Host
	return strings.HasSuffix(domain, "."+ds.suffix) || domain == ds.suffix
}

func (ds *DomainSuffix) Adapter() string {
	return ds.adapter
}

func (ds *DomainSuffix) Payload() string {
	return ds.suffix
}

func (ds *DomainSuffix) ShouldResolveIP() bool {
	return false
}

func NewDomainSuffix(suffix string, adapter string) *DomainSuffix {
	return &DomainSuffix{
		Base:    &Base{},
		suffix:  strings.ToLower(suffix),
		adapter: adapter,
	}
}

var _ C.Rule = (*DomainSuffix)(nil)
