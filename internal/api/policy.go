package api

type Method struct {
	Resource string `json:"resource"`
	Method   string `json:"method"`
	Notes    string `json:"notes,omitempty"`
}

var UserScopes = []string{
	"search:read",
	"channels:read",
	"channels:history",
	"groups:read",
	"groups:history",
	"im:read",
	"im:history",
	"mpim:read",
	"mpim:history",
	"users:read",
	"users:read.email",
	"users.profile:read",
	"team:read",
}

var ReadMethods = []Method{
	{Resource: "auth", Method: "auth.test", Notes: "cheap read-only reachability check"},
	{Resource: "users", Method: "users.list", Notes: "resolve @handles for search query expansion"},
	{Resource: "users", Method: "users.info"},
	{Resource: "users", Method: "users.lookupByEmail"},
	{Resource: "channels", Method: "conversations.list"},
	{Resource: "channels", Method: "conversations.info"},
	{Resource: "search", Method: "search.messages"},
	{Resource: "messages", Method: "conversations.history"},
	{Resource: "threads", Method: "conversations.replies"},
	{Resource: "permalinks", Method: "chat.getPermalink"},
}
