package signer

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/fil-forge/filecoin-services/go/eip712"
	"github.com/fil-forge/libforge/commands/pdp/sign"
	signerclient "github.com/fil-forge/piri-signing-service/pkg/client"
	signertypes "github.com/fil-forge/piri-signing-service/pkg/types"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"

	"github.com/fil-forge/piri/pkg/service/proofs"
)

// proofServiceSigner wraps a remote signing-service client and obtains an
// /access/grant delegation via the proof service before each call. The
// delegation is attached to the resulting signing invocation as proof.
type proofServiceSigner struct {
	c            *signerclient.Client
	serviceDID   did.DID
	proofService proofs.ProofService
	httpClient   *client.HTTPClient
}

// NewProofServiceSigner constructs a SigningService that fetches access
// grants on demand and forwards signing requests to the remote signing
// service via signerclient.Client.
func NewProofServiceSigner(
	c *signerclient.Client,
	serviceDID did.DID,
	httpClient *client.HTTPClient,
	proofService proofs.ProofService,
) signertypes.SigningService {
	return &proofServiceSigner{
		c:            c,
		serviceDID:   serviceDID,
		proofService: proofService,
		httpClient:   httpClient,
	}
}

func (s *proofServiceSigner) grant(ctx context.Context, issuer ucan.Signer, cmd ucan.Command) (ucan.Delegation, error) {
	d, err := s.proofService.RequestAccess(
		ctx,
		issuer,
		s.serviceDID,
		cmd,
		nil,
		proofs.WithClient(s.httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("requesting access for %s: %w", cmd, err)
	}
	return d, nil
}

func (s *proofServiceSigner) SignCreateDataSet(
	ctx context.Context,
	issuer ucan.Signer,
	dataSet *big.Int,
	payee common.Address,
	metadata []eip712.MetadataEntry,
	proofsIn []ucan.Delegation,
	options ...invocation.Option,
) (*eip712.AuthSignature, error) {
	d, err := s.grant(ctx, issuer, sign.DataSetCreateCommand)
	if err != nil {
		return nil, err
	}
	all := append([]ucan.Delegation{}, proofsIn...)
	all = append(all, d)
	return s.c.SignCreateDataSet(ctx, issuer, dataSet, payee, metadata, all, options...)
}

func (s *proofServiceSigner) SignAddPieces(
	ctx context.Context,
	issuer ucan.Signer,
	dataSet *big.Int,
	nonce *big.Int,
	pieceData [][]byte,
	metadata [][]eip712.MetadataEntry,
	pieceProofs []sign.PieceProofs,
	proofContainer ucan.Container,
	proofsIn []ucan.Delegation,
	options ...invocation.Option,
) (*eip712.AuthSignature, error) {
	d, err := s.grant(ctx, issuer, sign.PiecesAddCommand)
	if err != nil {
		return nil, err
	}
	all := append([]ucan.Delegation{}, proofsIn...)
	all = append(all, d)
	return s.c.SignAddPieces(ctx, issuer, dataSet, nonce, pieceData, metadata, pieceProofs, proofContainer, all, options...)
}

func (s *proofServiceSigner) SignSchedulePieceRemovals(
	ctx context.Context,
	issuer ucan.Signer,
	dataSet *big.Int,
	pieceIds []*big.Int,
	proofsIn []ucan.Delegation,
	options ...invocation.Option,
) (*eip712.AuthSignature, error) {
	d, err := s.grant(ctx, issuer, sign.PiecesRemoveScheduleCommand)
	if err != nil {
		return nil, err
	}
	all := append([]ucan.Delegation{}, proofsIn...)
	all = append(all, d)
	return s.c.SignSchedulePieceRemovals(ctx, issuer, dataSet, pieceIds, all, options...)
}

func (s *proofServiceSigner) SignDeleteDataSet(
	ctx context.Context,
	issuer ucan.Signer,
	dataSet *big.Int,
	proofsIn []ucan.Delegation,
	options ...invocation.Option,
) (*eip712.AuthSignature, error) {
	d, err := s.grant(ctx, issuer, sign.DataSetDeleteCommand)
	if err != nil {
		return nil, err
	}
	all := append([]ucan.Delegation{}, proofsIn...)
	all = append(all, d)
	return s.c.SignDeleteDataSet(ctx, issuer, dataSet, all, options...)
}
