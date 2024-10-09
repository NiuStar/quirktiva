package constant

import (
	"encoding/json"
	"fmt"
)

// DNSModeMapping is a mapping for EnhancedMode enum
var DNSModeMapping = map[string]DNSMode{
	DNSNormal.String():   DNSNormal,
	DNSFakeIP.String():   DNSFakeIP,
	DNSSniffing.String(): DNSSniffing,
}

const (
	DNSNormal DNSMode = iota
	DNSFakeIP
	DNSMapping
	DNSSniffing
)

type DNSMode int

// UnmarshalYAML unserialize EnhancedMode with yaml
func (e *DNSMode) UnmarshalYAML(unmarshal func(any) error) error {
	var tp string
	if err := unmarshal(&tp); err != nil {
		return err
	}
	mode, exist := DNSModeMapping[tp]
	if !exist {
		return fmt.Errorf("invalid mode: %s", tp)
	}
	*e = mode
	return nil
}

// UnmarshalJSON unserialize EnhancedMode with json
func (e *DNSMode) UnmarshalJSON(data []byte) error {
	var tp string
	if err := json.Unmarshal(data, &tp); err != nil {
		return err
	}
	mode, exist := DNSModeMapping[tp]
	if !exist {
		return fmt.Errorf("invalid mode: %s", tp)
	}
	*e = mode
	return nil
}

// MarshalYAML serialize EnhancedMode with yaml
func (e DNSMode) MarshalYAML() (any, error) {
	return e.String(), nil
}

// MarshalJSON serialize EnhancedMode with json
func (e DNSMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e DNSMode) String() string {
	switch e {
	case DNSNormal:
		return "normal"
	case DNSFakeIP:
		return "fake-ip"
	case DNSMapping:
		return "redir-host"
	case DNSSniffing:
		return "sniffing"
	default:
		return "unknown"
	}
}
