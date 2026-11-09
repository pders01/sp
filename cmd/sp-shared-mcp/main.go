// sp-shared-mcp is an opt-in experimental stdio-only shared adapter. It never
// reads launcher/client configuration or selects an implicit store or date.
package main

import "github.com/pders01/sp/internal/sharedmcp"

func main() { sharedmcp.Main() }
