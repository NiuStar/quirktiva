package mitm

import (
	"errors"
	"strconv"
	"strings"

	regexp "github.com/dlclark/regexp2"
	"github.com/gofrs/uuid/v5"

	C "github.com/yaling888/clash/constant"
)

var errInvalid = errors.New("invalid rewrite rule")

type RewriteRule struct {
	id          string
	urlRegx     *regexp.Regexp
	ruleType    C.RewriteType
	ruleRegx    []*regexp.Regexp
	rulePayload []string
}

func (r *RewriteRule) ID() string {
	return r.id
}

func (r *RewriteRule) URLRegx() *regexp.Regexp {
	return r.urlRegx
}

func (r *RewriteRule) RuleType() C.RewriteType {
	return r.ruleType
}

func (r *RewriteRule) RuleRegx() []*regexp.Regexp {
	return r.ruleRegx
}

func (r *RewriteRule) RulePayload() []string {
	return r.rulePayload
}

func (r *RewriteRule) ReplaceURLPayload(matchSub []string) string {
	if len(r.RulePayload()) == 0 {
		return ""
	}
	url := r.RulePayload()[0]
	l := len(matchSub)
	if l < 2 {
		return url
	}

	for i := 1; i < l; i++ {
		url = strings.ReplaceAll(url, "$"+strconv.Itoa(i), matchSub[i])
	}
	return url
}

func (r *RewriteRule) ReplaceSubPayload(oldData string) (string, bool) {
	if r.ruleRegx == nil || r.rulePayload == nil {
		return oldData, false
	}

	var ok bool
	for i, payload, pl := 0, "", len(r.rulePayload); i < len(r.ruleRegx); i++ {
		regx := r.ruleRegx[i]
		if i < pl {
			payload = r.rulePayload[i]
		}

		// sub := r.ruleRegx.FindStringSubmatch(oldData) // std
		sub := findStringSubmatch(regx, oldData)
		l := len(sub)

		if l == 0 {
			continue
		}

		ok = true
		newPayload := payload
		for j := 1; j < l; j++ {
			newPayload = strings.ReplaceAll(newPayload, "$"+strconv.Itoa(j), sub[j])
		}

		oldData = strings.ReplaceAll(oldData, sub[0], newPayload)
	}

	return oldData, ok
}

func NewRewriteRule(urlRegx *regexp.Regexp, ruleType C.RewriteType, ruleRegx []*regexp.Regexp, rulePayload []string) *RewriteRule {
	id, _ := uuid.NewV4()
	return &RewriteRule{
		id:          id.String(),
		urlRegx:     urlRegx,
		ruleType:    ruleType,
		ruleRegx:    ruleRegx,
		rulePayload: rulePayload,
	}
}

var _ C.Rewrite = (*RewriteRule)(nil)
