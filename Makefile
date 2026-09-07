# The gate lives in `cvfile`, not here. This target exists only because
# bullseye convergence probes for a `bullseye` Make target; it delegates
# so there is exactly one definition of green.
.PHONY: bullseye

bullseye:
	cv bullseye
