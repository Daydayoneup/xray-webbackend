package service

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protowire"
)

// geoip.dat / geosite.dat 是 protobuf 格式：
//   GeoIPList   { repeated GeoIP entry = 1 }   GeoIP   { string country_code = 1; repeated CIDR cidr = 2; bool reverse_match = 3 }
//   CIDR        { bytes ip = 1; uint32 prefix = 2 }
//   GeoSiteList { repeated GeoSite entry = 1 } GeoSite { string country_code = 1; repeated Domain domain = 2 }
//   Domain      { Type type = 1; string value = 2; repeated Attribute attribute = 3 }  Type: Plain=0 Regex=1 Domain=2 Full=3
//   Attribute   { string key = 1; ... }
// 这里只做按需解析：只加载规则里用到的代码，避免把整个文件常驻内存。

type GeoDomain struct {
	Type  int
	Value string
	Attrs []string
}

func forEachField(b []byte, fn func(num protowire.Number, typ protowire.Type, v []byte, n uint64) error) error {
	for len(b) > 0 {
		num, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return fmt.Errorf("格式错误")
		}
		b = b[l:]
		switch typ {
		case protowire.BytesType:
			v, l := protowire.ConsumeBytes(b)
			if l < 0 {
				return fmt.Errorf("格式错误")
			}
			if err := fn(num, typ, v, 0); err != nil {
				return err
			}
			b = b[l:]
		case protowire.VarintType:
			v, l := protowire.ConsumeVarint(b)
			if l < 0 {
				return fmt.Errorf("格式错误")
			}
			if err := fn(num, typ, nil, v); err != nil {
				return err
			}
			b = b[l:]
		default:
			l := protowire.ConsumeFieldValue(num, typ, b)
			if l < 0 {
				return fmt.Errorf("格式错误")
			}
			b = b[l:]
		}
	}
	return nil
}

// entryCode 读出一个 entry 的 country_code。
func entryCode(entry []byte) string {
	code := ""
	forEachField(entry, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		if num == 1 && typ == protowire.BytesType {
			code = string(v)
		}
		return nil
	})
	return code
}

// scanEntries 遍历 dat 文件中的每个 entry，fn 返回 true 时停止。
func scanEntries(data []byte, fn func(code string, entry []byte) bool) error {
	stop := fmt.Errorf("stop")
	err := forEachField(data, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		if num == 1 && typ == protowire.BytesType && fn(strings.ToUpper(entryCode(v)), v) {
			return stop
		}
		return nil
	})
	if err == stop {
		return nil
	}
	return err
}

func parseGeoIP(entry []byte) []*net.IPNet {
	var out []*net.IPNet
	forEachField(entry, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		if num != 2 || typ != protowire.BytesType {
			return nil
		}
		var ip []byte
		var prefix uint64
		forEachField(v, func(n protowire.Number, t protowire.Type, b []byte, u uint64) error {
			if n == 1 && t == protowire.BytesType {
				ip = b
			} else if n == 2 && t == protowire.VarintType {
				prefix = u
			}
			return nil
		})
		if len(ip) == 4 || len(ip) == 16 {
			out = append(out, &net.IPNet{IP: net.IP(append([]byte(nil), ip...)), Mask: net.CIDRMask(int(prefix), len(ip)*8)})
		}
		return nil
	})
	return out
}

func parseGeoSite(entry []byte) []GeoDomain {
	var out []GeoDomain
	forEachField(entry, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		if num != 2 || typ != protowire.BytesType {
			return nil
		}
		d := GeoDomain{}
		forEachField(v, func(n protowire.Number, t protowire.Type, b []byte, u uint64) error {
			switch {
			case n == 1 && t == protowire.VarintType:
				d.Type = int(u)
			case n == 2 && t == protowire.BytesType:
				d.Value = string(b)
			case n == 3 && t == protowire.BytesType:
				d.Attrs = append(d.Attrs, entryCode(b))
			}
			return nil
		})
		out = append(out, d)
		return nil
	})
	return out
}

// GeoDB 按需加载 geoip/geosite 代码，带缓存（文件变化时失效）。
type GeoDB struct {
	Dir   string
	mu    sync.Mutex
	stamp string
	ip    map[string][]*net.IPNet
	site  map[string][]GeoDomain
	re    map[string]*regexp.Regexp
	cat   map[string][]GeoCode
}

// GeoCode 是 geosite/geoip 中的一个分类及其条目数。
type GeoCode struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

func NewGeoDB(dir string) *GeoDB { return &GeoDB{Dir: dir} }

func (g *GeoDB) checkStamp() {
	stamp := ""
	for _, f := range []string{"geoip.dat", "geosite.dat"} {
		if st, err := os.Stat(filepath.Join(g.Dir, f)); err == nil {
			stamp += fmt.Sprintf("%d-%d;", st.Size(), st.ModTime().UnixNano())
		}
	}
	if stamp != g.stamp {
		g.stamp, g.ip, g.site, g.re = stamp, map[string][]*net.IPNet{}, map[string][]GeoDomain{}, map[string]*regexp.Regexp{}
		g.cat = map[string][]GeoCode{}
	}
}

func (g *GeoDB) load(file, code string, fn func(entry []byte)) (bool, error) {
	data, err := os.ReadFile(filepath.Join(g.Dir, file))
	if err != nil {
		return false, fmt.Errorf("找不到 %s（%s）", file, g.Dir)
	}
	found := false
	err = scanEntries(data, func(c string, entry []byte) bool {
		if c == code {
			fn(entry)
			found = true
			return true
		}
		return false
	})
	return found, err
}

func (g *GeoDB) GeoIP(code string) ([]*net.IPNet, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkStamp()
	code = strings.ToUpper(code)
	if v, ok := g.ip[code]; ok {
		return v, nil
	}
	var nets []*net.IPNet
	found, err := g.load("geoip.dat", code, func(e []byte) { nets = parseGeoIP(e) })
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("geoip 中没有 %s", code)
	}
	g.ip[code] = nets
	return nets, nil
}

func (g *GeoDB) GeoSite(code string) ([]GeoDomain, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkStamp()
	code = strings.ToUpper(code)
	if v, ok := g.site[code]; ok {
		return v, nil
	}
	var ds []GeoDomain
	found, err := g.load("geosite.dat", code, func(e []byte) { ds = parseGeoSite(e) })
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("geosite 中没有 %s", code)
	}
	g.site[code] = ds
	return ds, nil
}

// Catalog 列出 geosite.dat（kind=site）或 geoip.dat（kind=ip）中的全部分类（小写），按名称排序。
func (g *GeoDB) Catalog(kind string) ([]GeoCode, error) {
	file := map[string]string{"site": "geosite.dat", "ip": "geoip.dat"}[kind]
	if file == "" {
		return nil, fmt.Errorf("kind 只能是 site / ip")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkStamp()
	if v, ok := g.cat[kind]; ok {
		return v, nil
	}
	data, err := os.ReadFile(filepath.Join(g.Dir, file))
	if err != nil {
		return nil, fmt.Errorf("找不到 %s（%s）", file, g.Dir)
	}
	out := []GeoCode{}
	err = scanEntries(data, func(code string, entry []byte) bool {
		n := 0
		forEachField(entry, func(num protowire.Number, typ protowire.Type, _ []byte, _ uint64) error {
			if num == 2 && typ == protowire.BytesType {
				n++
			}
			return nil
		})
		out = append(out, GeoCode{Code: strings.ToLower(code), Count: n})
		return false
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	g.cat[kind] = out
	return out, nil
}

// Entries 返回某个分类的条目（Xray 规则写法），q 非空时按子串过滤，最多 limit 条；total 为过滤后的总数。
func (g *GeoDB) Entries(kind, code, q string, limit int) ([]string, int, error) {
	var all []string
	switch kind {
	case "site":
		ds, err := g.GeoSite(code)
		if err != nil {
			return nil, 0, err
		}
		for _, d := range ds {
			all = append(all, GeoEntryString(d))
		}
	case "ip":
		nets, err := g.GeoIP(code)
		if err != nil {
			return nil, 0, err
		}
		for _, n := range nets {
			all = append(all, n.String())
		}
	default:
		return nil, 0, fmt.Errorf("kind 只能是 site / ip")
	}
	q = strings.ToLower(strings.TrimSpace(q))
	out := []string{}
	total := 0
	for _, e := range all {
		if q != "" && !strings.Contains(strings.ToLower(e), q) {
			continue
		}
		total++
		if len(out) < limit {
			out = append(out, e)
		}
	}
	return out, total, nil
}

func (g *GeoDB) regex(expr string) *regexp.Regexp {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r, ok := g.re[expr]; ok {
		return r
	}
	r, _ := regexp.Compile(expr)
	if g.re == nil {
		g.re = map[string]*regexp.Regexp{}
	}
	g.re[expr] = r
	return r
}

// ValidateGeoFiles 校验下载的 dat 文件可解析且包含 CN。
func ValidateGeoFiles(geoip, geosite []byte) error {
	ok := false
	if err := scanEntries(geoip, func(c string, _ []byte) bool { ok = ok || c == "CN"; return ok }); err != nil || !ok {
		return fmt.Errorf("geoip.dat 无效")
	}
	ok = false
	if err := scanEntries(geosite, func(c string, _ []byte) bool { ok = ok || c == "CN"; return ok }); err != nil || !ok {
		return fmt.Errorf("geosite.dat 无效")
	}
	return nil
}
