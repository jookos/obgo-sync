.PHONY: up qmd-image

up:
	docker compose --env-file env.local up

qmd-image:
	docker build -t jookos.org/qmd -f Dockerfile.qmd-node20 .
