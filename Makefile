VERSION := $(shell  git log -1 --format="%ad+%h" --date=format:"%Y.%j.%H%M")

project.yaml: project.yaml.in
	@sed -e 's|__VERSION__|$(VERSION)|g' -e 's|\.0*|.|g' project.yaml.in > $@