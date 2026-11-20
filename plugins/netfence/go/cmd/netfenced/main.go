// netfenced is the netfence plugin engine: a localhost-only management API
// plus an optional DNS sinkhole listener. Fully static (CGO_ENABLED=0) for the
// iKuai uClibc firmware; no kernel module, no root required for the API tier.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ikuai4/netfence/internal/api"
	"github.com/ikuai4/netfence/internal/core"
)

// Version is injected at build time with -X main.Version=<metadata.version>.
// build.py's version-gate reads this string back out of the binary.
var Version = "dev"

func main() {
	var (
		apiAddr  = flag.String("api", "127.0.0.1:18111", "management API listen address")
		dnsAddr  = flag.String("dns", "", "DNS sinkhole listen address (empty = disabled)")
		upstream = flag.String("upstream", "223.5.5.5:53", "upstream DNS for allowed names")
		cfgDir   = flag.String("config", "./config", "config dir on the plugin storage volume")
		showVer  = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVer {
		fmt.Println(Version)
		return
	}

	eng, err := core.NewEngine(core.Config{Dir: *cfgDir, ACL: core.DefaultACLConfig()})
	if err != nil {
		log.Fatalf("netfence: engine init: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", api.New(eng, Version, *cfgDir).Handler())
	srv := &http.Server{
		Addr:              *apiAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("netfence %s API on %s (config=%s)", Version, *apiAddr, *cfgDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("netfence: api: %v", err)
		}
	}()

	if *dnsAddr != "" {
		go func() {
			if err := serveDNS(*dnsAddr, *upstream, eng); err != nil {
				log.Printf("netfence: dns sinkhole stopped: %v", err)
			}
		}()
	}

	// SIGHUP = hot reload config without dropping the listener (policy edits
	// from the UI take effect immediately, no restart / no traffic gap).
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	for s := range sig {
		if s == syscall.SIGHUP {
			if err := eng.Reload(*cfgDir); err != nil {
				log.Printf("netfence: reload failed: %v", err)
			} else {
				log.Printf("netfence: config reloaded")
			}
			continue
		}
		log.Printf("netfence: shutting down (%v)", s)
		_ = srv.Close()
		return
	}
}

// serveDNS is a minimal DNS forwarder with policy-based interception. It is
// deliberately small: parse the question, ask the engine, and either answer
// with a sinkhole (0.0.0.0 / NXDOMAIN-style REFUSED) or relay to upstream.
// This is the enforcement point that turns "上网行为管理" decisions into actual
// network behavior (the same role Pi-hole/AdGuard Home's DNS server plays).
func serveDNS(addr, upstream string, eng *core.Engine) error {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	log.Printf("netfence: DNS sinkhole on udp/%s -> %s", addr, upstream)
	buf := make([]byte, 4096)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		q := append([]byte(nil), buf[:n]...)
		go handleDNS(conn, from, q, upstream, eng)
	}
}

func handleDNS(conn net.PacketConn, from net.Addr, q []byte, upstream string, eng *core.Engine) {
	name, ok := dnsQuestion(q)
	if !ok {
		return
	}
	clientIP := ""
	if u, ok := from.(*net.UDPAddr); ok {
		clientIP = u.IP.String()
	}
	d := eng.Decide(core.Request{ClientIP: clientIP, Domain: name, At: time.Now()})
	if d.Blocked() {
		_, _ = conn.WriteTo(sinkholeResponse(q), from)
		return
	}
	// relay allowed query to upstream, preserving the transaction id
	up, err := net.DialTimeout("udp", upstream, 3*time.Second)
	if err != nil {
		_, _ = conn.WriteTo(sinkholeResponse(q), from)
		return
	}
	defer up.Close()
	_ = up.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = up.Write(q); err != nil {
		return
	}
	resp := make([]byte, 4096)
	n, err := up.Read(resp)
	if err != nil {
		return
	}
	_, _ = conn.WriteTo(resp[:n], from)
}

// dnsQuestion extracts the first QNAME from a DNS query.
func dnsQuestion(msg []byte) (string, bool) {
	if len(msg) < 12 {
		return "", false
	}
	qd := binary.BigEndian.Uint16(msg[4:6])
	if qd == 0 {
		return "", false
	}
	var labels []string
	for i := 12; i < len(msg); {
		l := int(msg[i])
		if l == 0 {
			i++
			break
		}
		if l&0xc0 == 0xc0 || i+1+l > len(msg) {
			return "", false
		}
		labels = append(labels, string(msg[i+1:i+1+l]))
		i += 1 + l
	}
	if len(labels) == 0 {
		return "", false
	}
	return strings.Join(labels, "."), true
}

// sinkholeResponse answers with a valid DNS response carrying no A record
// (RCODE=0, ANCOUNT=0): the client resolves to nothing, without the browser
// "server failure" noise an NXDOMAIN/REFUSED can produce on some routers.
func sinkholeResponse(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	r := append([]byte(nil), q...)
	r[2] |= 0x80 // QR=1
	r[3] = (r[3] & 0xf0) | 0x0
	binary.BigEndian.PutUint16(r[6:8], 0)  // ANCOUNT
	binary.BigEndian.PutUint16(r[8:10], 0) // NSCOUNT
	binary.BigEndian.PutUint16(r[10:12], 0)
	return r
}
