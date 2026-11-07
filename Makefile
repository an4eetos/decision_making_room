# The server lives in backend/. Every target forwards there, so `make run`
# from the repository root is still the one command.
.PHONY: all
all:
	@$(MAKE) --no-print-directory -C backend run

%:
	@$(MAKE) --no-print-directory -C backend $@
