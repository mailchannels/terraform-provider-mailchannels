// Command stubapi serves the stateful Email API stub used by the
// end-to-end test. It is test tooling, not part of the provider.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/stubapi"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	flag.Parse()
	stub, err := stubapi.New()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Email API stub listening on http://%s%s", *addr, stubapi.BasePath)
	log.Fatal(http.ListenAndServe(*addr, stub))
}
