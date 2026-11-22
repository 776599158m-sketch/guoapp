package core

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

var nonPublicCDNNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func protectedCDNHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, domain := range []string{"tuafjz.cn", "zdmhyg.cn", "lkkwip.cn", "bnfuiu.cn"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func sourceEntryDNSHost(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(host, ".")) {
	case "dsd901.com", "www.dsd901.com", "dsd901.lol", "www.dsd901.lol", "ygdj15.com", "www.ygdj15.com",
		"huangguo.video", "www.huangguo.video":
		return true
	default:
		return false
	}
}

// mediaCDNHost 标记按 GeoDNS 分区的媒体分发域名：它们的境外节点在境内不可达，
// 解析时必须先走本地线路，不能用国外 ECS 取到的地址。黄果旧版的分片全部由
// lkkwip.cn 下发，实测同域名国内地址返回 200、境外地址 TLS 握手即失败。
func mediaCDNHost(host string) bool {
	return protectedCDNHost(host)
}

func validateCDNAddresses(addresses []string) error {
	if len(addresses) == 0 {
		return errors.New("CDN 没有可用的公网地址")
	}
	for _, value := range addresses {
		address, err := netip.ParseAddr(value)
		if err != nil || address.Zone() != "" {
			return errors.New("CDN 返回无效地址")
		}
		address = address.Unmap()
		if !address.IsGlobalUnicast() || address.IsPrivate() {
			return fmt.Errorf("CDN 返回非公网地址：%s", address)
		}
		for _, network := range nonPublicCDNNetworks {
			if network.Contains(address) {
				return fmt.Errorf("CDN 返回保留地址：%s", address)
			}
		}
	}
	return nil
}
