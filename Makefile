all:
	podman build -t haih/spiffe-csi-driver:latest .

push:
	podman push haih/spiffe-csi-driver:latest
