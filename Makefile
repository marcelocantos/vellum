# cv gate (cvfile) is the only definition of green. This target exists
# solely so tools that call `make bullseye` (the bullseye/cv convergence
# hook) get the same oracle; never add a second recipe here (ENT-001).
.PHONY: bullseye

bullseye:
	cv bullseye
