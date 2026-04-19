package types

// APIEndpoint represents an HTTP route registration discovered from source code.
type APIEndpoint struct {
	Method     string   `json:"method"`      // "GET", "POST", "PUT", "DELETE", "PATCH"
	Path       string   `json:"path"`        // "/api/v1/users/:id"
	Handler    string   `json:"handler"`     // qualified handler function name
	Middleware []string `json:"middleware"`   // qualified middleware function names
	Group      string   `json:"group"`       // route group prefix (e.g. "/api/v1")
	FilePath   string   `json:"file_path"`   // file where route is registered
	Line       int      `json:"line"`        // line of registration
}

// APIParam represents a request parameter extracted from handler code.
type APIParam struct {
	Name   string `json:"name"`   // "id", "page"
	In     string `json:"in"`     // "path", "query", "header", "body"
	Source string `json:"source"` // extraction call: "c.Param", "c.QueryParam", "c.Bind"
}

// APIBinding holds the request/response binding information for an endpoint.
type APIBinding struct {
	Params        []APIParam `json:"params"`
	RequestType   string     `json:"request_type,omitempty"`   // struct name from c.Bind(&req)
	ResponseTypes []string   `json:"response_types,omitempty"` // struct names from c.JSON(status, data)
}

// APISurface is the complete API surface discovered for a repository.
type APISurface struct {
	Endpoints []APIEndpointDetail `json:"endpoints"`
	Timing    TimingInfo          `json:"timing"`
}

// APIEndpointDetail is a fully analyzed endpoint with taint and exposure data.
type APIEndpointDetail struct {
	Endpoint      APIEndpoint    `json:"endpoint"`
	Binding       APIBinding     `json:"binding"`
	TaintFlows    []TaintFlow    `json:"taint_flows,omitempty"`
	ExposedFields []ExposedField `json:"exposed_fields,omitempty"`
	AuthChain     []string       `json:"auth_chain,omitempty"`
	DataStores    []string       `json:"data_stores,omitempty"`
}

// TaintFlow describes a data flow from an API input parameter to a sink function.
type TaintFlow struct {
	InputParam string   `json:"input_param"` // e.g. "c.Param(\"id\")"
	SinkFunc   string   `json:"sink_func"`   // qualified sink function
	Path       []string `json:"path"`        // qualified function chain
	Sanitized  bool     `json:"sanitized"`
}

// ExposedField describes a response field with PII sensitivity information.
type ExposedField struct {
	FieldName string `json:"field_name"`
	JSONTag   string `json:"json_tag"`
	IsPII     bool   `json:"is_pii"`
	PIIKind   string `json:"pii_kind,omitempty"` // "email", "phone", "ssn"
}
