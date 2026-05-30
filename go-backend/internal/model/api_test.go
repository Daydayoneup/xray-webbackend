package model

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

func TestInboundInValidation(t *testing.T) {
	tests := []struct {
		name  string
		input InboundIn
		valid bool
	}{
		{"valid socks", InboundIn{Protocol: "socks", Port: 1080}, true},
		{"valid http", InboundIn{Protocol: "http", Port: 8080}, true},
		{"invalid protocol", InboundIn{Protocol: "ssh", Port: 1080}, false},
		{"port too low", InboundIn{Protocol: "socks", Port: 0}, false},
		{"port too high", InboundIn{Protocol: "socks", Port: 65536}, false},
		{"missing protocol", InboundIn{Port: 1080}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(tt.input)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected invalid, got valid")
			}
		})
	}
}

func TestAuthInValidation(t *testing.T) {
	a := AuthIn{User: "admin", Password: ""}
	if err := validate.Struct(a); err == nil {
		t.Error("expected validation error for empty password")
	}
	a2 := AuthIn{User: "", Password: "secret"}
	if err := validate.Struct(a2); err == nil {
		t.Error("expected validation error for empty user")
	}
}

func TestProxyInValidation(t *testing.T) {
	// socks/http: port 0 is ok since host:port validation moved to handler
	valid := ProxyIn{Protocol: "socks", Host: "10.0.0.1", Port: 1080}
	if err := validate.Struct(valid); err != nil {
		t.Errorf("valid proxy should pass: %v", err)
	}
	// vmess without link: allowed (handler will reject if no link)
	vmess := ProxyIn{Protocol: "vmess", Link: "vmess://test"}
	if err := validate.Struct(vmess); err != nil {
		t.Errorf("vmess with link should pass: %v", err)
	}
}
