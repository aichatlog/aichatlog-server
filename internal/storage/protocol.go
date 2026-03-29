package storage

// ProtocolVersion is the aichatlog-protocol version this server was built against.
// See: https://github.com/aichatlog/aichatlog-protocol
const ProtocolVersion = "0.6.0"

// SupportedWireVersions lists the ConversationObject version integers this server accepts.
var SupportedWireVersions = []int{1, 2}
