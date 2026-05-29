package ucanhandlers

import (
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/execution"
)

// UnsupportedCapabilityErrorName is the stable receipt-failure name for
// invocations whose subject does not match the agent operating this
// server. Mirrors the legacy NewUnsupportedCapabilityError check on
// cap.With() vs iCtx.ID().DID().
const UnsupportedCapabilityErrorName = "UnsupportedCapability"

// RequireSubject returns a named receipt-failure error if the invocation
// subject differs from agent. Per-handler usage:
//
//	if err := requireSubject(req, deps.ID.DID()); err != nil {
//		return rsp.SetFailure(err)
//	}
func RequireSubject(req execution.Request, agent did.DID) error {
	if req.Invocation().Task().Subject() != agent {
		return errors.New(
			UnsupportedCapabilityErrorName,
			"capability subject %s is not %s",
			req.Invocation().Task().Subject(), agent,
		)
	}
	return nil
}
