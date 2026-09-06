# Owner gate (think 🎯T8). vellum has no CI — the GitHub Actions
# workflows were deliberately deleted on 2026-08-27 — so the pre-push hook
# (scripts/hooks/pre-push) is the entire gate. It runs `make gate`, which
# runs `cv gate`, and refuses the push when it is red.
#
# The gate itself lives in `cvfile`, not here: fmt, vet, the suite under
# VELLUM_REQUIRE_DEPS=1 with -race, the zero-non-pasteboard skip census, and
# the CLI contract. This target is the standard fleet entry point onto it, so
# the hook needs no repo-specific knowledge.
.PHONY: gate hooks bullseye

gate:
	cv gate

# Wire the pre-push gate. A relative core.hooksPath resolves against
# whichever worktree the push runs from, so one setting covers them all.
hooks:
	@git config core.hooksPath scripts/hooks && chmod +x scripts/hooks/* && \
	 echo "✓ core.hooksPath=scripts/hooks (pre-push runs make gate)"

bullseye: gate
	@dirty=$$(git status --porcelain | grep -vE 'bullseye\.yaml$$' || true); \
	if [ -z "$$dirty" ]; then echo "✓ working tree clean"; \
	else \
	  echo ""; \
	  echo "================================================================"; \
	  echo "⚠  DIRTY WORKING TREE"; \
	  echo ""; \
	  echo "Warning only — invariants still pass (exit 0)."; \
	  echo "Look at the files below before starting a new target."; \
	  echo "Leftover work from a different objective → park it in a commit first."; \
	  echo "This session's WIP on the recommended target → continue."; \
	  echo "================================================================"; \
	  echo "$$dirty"; \
	  echo "================================================================"; \
	  echo ""; \
	fi
