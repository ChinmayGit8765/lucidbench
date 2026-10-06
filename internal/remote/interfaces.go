package remote

import (
	"net"
	"sort"
)

// Interface is one address the remote could bind to, for the picker.
type Interface struct {
	Name    string `json:"name"`    // the OS's interface name
	Address string `json:"address"` // an IPv4 or IPv6 address
	// Kind is loopback (this computer only, for testing), private (a LAN
	// address), tailnet (100.64.0.0/10, a tailscale address) or other.
	Kind string `json:"kind"`
}

var tailnetRange = &net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}

// IsTailnet reports whether ip is in 100.64.0.0/10, the range tailscale
// gives its nodes.
func IsTailnet(ip net.IP) bool {
	return tailnetRange.Contains(ip)
}

func kindOf(ip net.IP) string {
	switch {
	case ip.IsLoopback():
		return "loopback"
	case IsTailnet(ip):
		return "tailnet"
	case ip.IsPrivate():
		return "private"
	default:
		return "other"
	}
}

// SystemInterfaces lists the up interfaces' unicast addresses, without
// link-local IPv6 ones (they need a zone and phones rarely reach them).
func SystemInterfaces() ([]Interface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Interface
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.IsLinkLocalUnicast() || n.IP.IsUnspecified() || n.IP.IsMulticast() {
				continue
			}
			ip := n.IP
			if v4 := ip.To4(); v4 != nil {
				ip = v4
			}
			out = append(out, Interface{Name: ifc.Name, Address: ip.String(), Kind: kindOf(ip)})
		}
	}
	rank := map[string]int{"private": 0, "tailnet": 1, "other": 2, "loopback": 3}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Kind] < rank[out[j].Kind] })
	return out, nil
}
