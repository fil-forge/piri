package main

import (
	"net/http"

	"github.com/fil-forge/piri/cmd/lambda"
	"github.com/fil-forge/piri/pkg/aws"
	"github.com/fil-forge/piri/pkg/server"
)

func main() {
	lambda.StartHTTPHandler(makeHandler)
}

func makeHandler(cfg aws.Config) (http.Handler, error) {
	return server.NewHandler(cfg.Signer), nil
}
