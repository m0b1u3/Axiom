module axiom/code-audit-agent

go 1.24.0

require (
	axiom/contracts v0.0.0
	axiom/sourceidentity v0.0.0
)

require (
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace axiom/contracts => ../contracts/go

replace axiom/sourceidentity => ../shared/sourceidentity
