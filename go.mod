// Module: container-runtime-tools
// Purpose: small, dependency-light tooling for running and auditing containers.
// The only non-stdlib dependency is golang.org/x/sys, used for Linux namespaces.
module github.com/sukonoe36/container-runtime-tools

go 1.22

require golang.org/x/sys v0.20.0
