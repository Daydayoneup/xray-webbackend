package model

import "encoding/json"

// Auth — username/password auth. Serializes password as "pass" in JSON.
type Auth struct {
	User     string `json:"user"`
	Password string `json:"-"`
}

func (a Auth) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}{a.User, a.Password})
}

func (a *Auth) UnmarshalJSON(b []byte) error {
	var raw struct {
		User     string `json:"user"`
		Pass     string `json:"pass"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.User = raw.User
	a.Password = raw.Pass
	if a.Password == "" {
		a.Password = raw.Password // populate_by_name compat
	}
	return nil
}

type PasswordRec struct {
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

type Subscription struct {
	ID        int    `json:"id"`
	URL       string `json:"url"`
	Remarks   string `json:"remarks"`
	Status    string `json:"status"`
	FetchedAt int64  `json:"fetched_at"`
}

type Node struct {
	Tag      string         `json:"tag"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Host     string         `json:"host"`
	Port     int            `json:"port"`
	Latency  *int           `json:"latency,omitempty"`
	Outbound map[string]any `json:"outbound"`
}

type Inbound struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	UDP      bool   `json:"udp"`
	Auth     *Auth  `json:"auth,omitempty"`
}

type Proxy struct {
	Tag         string         `json:"tag"`
	Name        string         `json:"name"`
	Protocol    string         `json:"protocol"`
	Host        string         `json:"host"`
	Port        int            `json:"port"`
	Auth        *Auth          `json:"auth,omitempty"`
	Link        string         `json:"link,omitempty"`
	RawOutbound map[string]any `json:"rawOutbound,omitempty"`
}

type Balancer struct {
	Tag      string   `json:"tag"`
	Name     string   `json:"name"`
	Nodes    []string `json:"nodes"`
	Strategy string   `json:"strategy"`
}

type Rule struct {
	ID       int    `json:"id"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Outbound string `json:"outbound"`
	Enabled  bool   `json:"enabled"`
}

type PanelState struct {
	Password        *PasswordRec   `json:"password,omitempty"`
	Subscriptions   []Subscription `json:"subscriptions"`
	Nodes           []Node         `json:"nodes"`
	Inbounds        []Inbound      `json:"inbounds"`
	Proxies         []Proxy        `json:"proxies"`
	Balancers       []Balancer     `json:"balancers"`
	Rules           []Rule         `json:"rules"`
	DefaultOutbound string         `json:"default_outbound"`
	InboundSeq      int            `json:"inbound_seq"`
	ProxySeq        int            `json:"proxy_seq"`
	BalancerSeq     int            `json:"balancer_seq"`
	SubSeq          int            `json:"sub_seq"`
}
