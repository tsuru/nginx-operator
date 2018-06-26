.PHONY: test deploy local

test:
	go test ./...

deploy:
	kubectl apply -f deploy/

local: deploy
	operator-sdk up local