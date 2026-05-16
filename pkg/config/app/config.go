package app

// AppConfig is the root configuration for the entire application
type AppConfig struct {
	// Identity configuration
	Identity IdentityConfig

	// Server configuration
	Server ServerConfig

	// DataDir is the root data directory for on-disk state.
	DataDir string

	// TempDir is the root temporary directory for ephemeral state.
	TempDir string

	// Database configures the database backend (sqlite or postgres) used by
	// every logical database in the deployment.
	Database DatabaseConfig

	// ObjectStore configures the object-store backend (memory, filesystem, or s3)
	// used by the bulk stores.
	ObjectStore ObjectStoreConfig

	// Configuration specific for UCAN operations
	UCANService UCANServiceConfig

	// Configuration specific for PDP operations
	PDPService PDPServiceConfig

	// Telemetry configuration
	Telemetry TelemetryConfig

	//
	// Configs below are not exposed to users, they are hard coded with defaults
	// their purpose is to allow configurable configuration injection in tests
	// They may be exposed to users later
	Replicator ReplicatorConfig
}
