package errorcode

// Code represents the standard error codes defined by the system.
type Code int

const (
	// 1xxx: General Errors
	Unknown      Code = 1000
	InvalidParam Code = 1100
	InternalErr  Code = 1200

	// 2xxx: Infrastructure Errors
	DatabaseErr Code = 2100
	QueueErr    Code = 2200
	StorageErr  Code = 2300
	NetworkErr  Code = 2400

	// 3xxx: Pipeline Errors
	FetchFailed     Code = 3100
	TransformFailed Code = 3200
	ParseFailed     Code = 3300
	SaveFailed      Code = 3400

	// 4xxx: Discovery Errors
	LLMError    Code = 4100
	SearchError Code = 4200
)
