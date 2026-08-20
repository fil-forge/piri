package replica

// Module wires the blob/replica/* capabilities into the body-CAR RPC
// server. ReplicaAllocateDeps embeds blob.AllocateDeps, so this module
// requires blob.Module to be composed alongside it for the AllocateDeps
// adapter bindings.
//
// Disabled during the UCAN 1.0 migration — re-enable: see #15.
// var Module = fx.Module("ucan/blob/replica",
//	fx.Provide(
//		ucanhandlers.ProvideRPC(NewReplicaAllocateHandler),
//	),
// )
