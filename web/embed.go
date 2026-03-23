package web

import _ "embed"

//go:embed dashboard.html
var DashboardHTML []byte

//go:embed favicon.ico
var FaviconICO []byte
