
.PHONY: release
release:
	./scripts/aws-k8s-tester.build.release.sh

clean:
	rm -rf ./bin
	find **/*.generated.yaml -print0 | xargs -0 rm -f || true
	find **/*.coverprofile -print0 | xargs -0 rm -f || true
