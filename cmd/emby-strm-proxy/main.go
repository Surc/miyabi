package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/strm"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8397", "HTTP listen address for the Emby proxy")
	emby := flag.String("emby", "", "Emby server origin, for example http://127.0.0.1:8096")
	miyabi := flag.String("miyabi", "", "Miyabi base URL used by the existing STRM files")
	trusted := flag.String("trusted-proxies", "", "Comma-separated IPs or CIDRs of trusted front proxies")
	flag.Parse()

	handler, err := strm.NewEmbyProxy(*emby, *miyabi, strings.Split(*trusted, ",")...)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("Emby STRM proxy listening on %s", *listen)
	log.Fatal(server.ListenAndServe())
}
