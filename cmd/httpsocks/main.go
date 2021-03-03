package main

import (
	"context"
	"crypto/tls"
	"flag"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/proxy"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	log.SetOutput(os.Stdout)

	debug := flag.Bool("debug", true, "Enable debug output")
	upstreamAddr := flag.String("upstream", "https://ipinfo.io:443", "Upstream host URL")
	socksAddr := flag.String("proxy", "localhost:9050", "SOCKS5 proxy address")
	insecureTLS := flag.Bool("insecure", false, "Ignore TLS certificate errors")
	listenAddr := flag.String("bind", "0.0.0.0:10443", "Address to listen on")

	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
		log.Debug("Debug logging enabled")
	} else {
		log.SetLevel(log.InfoLevel)
	}

	remote, err := url.Parse(*upstreamAddr)
	check(err)
	log.Debug("Upstream host: ", remote.Host)
	log.Debug("Upstream proto: ", remote.Scheme)

	if *insecureTLS {
		log.Debug("Warning: insecure mode is on; TLS validation errors will be ignored")
	}

	rp := httputil.NewSingleHostReverseProxy(remote)
	rp.Transport = NewProxyTransport(*socksAddr, *insecureTLS)

	server := ReverseHttpServer{rp, remote.Host}
	err = http.ListenAndServe(*listenAddr, &server)
	check(err)
}

type ReverseHttpServer struct {
	rp *httputil.ReverseProxy
	vhost string
}

func (s *ReverseHttpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Debug(r.URL)
	r.Host = s.vhost
	s.rp.ServeHTTP(w, r)
}

type ProxyTransport struct{
	underlyingTransport *http.Transport
}

func (t *ProxyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	delete(r.Header, "X-Forwarded-For") // Fuck off.
	resp, err := t.underlyingTransport.RoundTrip(r)
	return resp, err
}

func NewProxyTransport(socksAddr string, insecureTLS bool) *ProxyTransport {
	baseDialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 10 * time.Second,
	}

	dialSocksProxy, err := proxy.SOCKS5("tcp", socksAddr, nil, baseDialer)
	check(err)
	var dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	if contextDialer, ok := dialSocksProxy.(proxy.ContextDialer); ok {
		dialContext = contextDialer.DialContext
	} else {
		panic("type assertion failed")
	}

	underlying := http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialContext,
		MaxIdleConns:          10,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
	}

	if insecureTLS {
		underlying.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &ProxyTransport{&underlying}
}
