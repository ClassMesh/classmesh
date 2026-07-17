module github.com/ClassMesh/classmesh

go 1.25.0

require (
	golang.org/x/text v0.38.0
	gopkg.in/yaml.v3 v3.0.1
)

retract [v0.1.0, v0.1.1] // CLI-era repository tags, no root module
