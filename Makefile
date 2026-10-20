# cv gate (cvfile) is the only definition of green. The bullseye target
# exists solely so tools that call `make bullseye` (the bullseye/cv
# convergence hook) get the same oracle; never add a second gate recipe
# here (ENT-001).
#
# verify-release is not a gate: it checks the INSTALLED release on Colossus
# (Homebrew vellum + supervisord vellum-view-release) after `brew upgrade
# vellum`. Override the expected version with VERSION=x.y.z (default: the
# latest git tag).
.PHONY: bullseye verify-release

bullseye:
	cv bullseye

verify-release:
	./scripts/verify-release.sh $(VERSION)
