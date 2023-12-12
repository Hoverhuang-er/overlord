export GO111MODULE=on

test:
	./scripts/codecov.sh

build:
	cd cmd/apicli && go build && cd -
	cd cmd/apiserver && go build && cd -
	cd cmd/balancer && go build && cd -
	cd cmd/executor && go build && cd -
	cd cmd/proxy && go build && cd -
	cd cmd/scheduler && go build && cd -
	cd cmd/anzi && go build && cd -

push:
	git add . && git commit -m "update" && git tag -a v100.0.1 -m "v100.0.1" -f && git push origin v100.0.1 --tags -f && git push

tag:
	git add . && git commit -m 'add coa feature 1a(password + TLS) + 2a + 3 +4 + web(caddy) + skywalking-go auto-instrument' && git push