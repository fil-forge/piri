package main

import (
	"net/http"

	ucanserver "github.com/fil-forge/go-ucanto/server"

	logging "github.com/ipfs/go-log/v2"

	"github.com/fil-forge/piri/cmd/lambda"
	"github.com/fil-forge/piri/internal/telemetry"
	"github.com/fil-forge/piri/pkg/aws"
	"github.com/fil-forge/piri/pkg/principalresolver"
	"github.com/fil-forge/piri/pkg/service/storage"
)

var log = logging.Logger("lambda/postroot")

func main() {
	lambda.StartHTTPHandler(makeHandler)
}

func makeHandler(cfg aws.Config) (http.Handler, error) {
	service, err := aws.Construct(cfg)
	if err != nil {
		return nil, err
	}

	presolv, err := principalresolver.NewMapResolver(cfg.PrincipalMapping)
	if err != nil {
		return nil, err
	}

	server, err := storage.NewUCANServer(service, ucanserver.WithPrincipalResolver(presolv.ResolveDIDKey))
	if err != nil {
		return nil, err
	}

	handler := storage.NewHandler(server)
	return telemetry.NewErrorReportingHandler(func(w http.ResponseWriter, r *http.Request) error {
		err := handler(aws.NewHandlerContext(w, r))
		if err != nil {
			log.Error(err)
		}
		return err
	}), nil
}
