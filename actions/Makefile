REGISTRY?=platinasystems.com
REPOSITORY?=tinkerbell-actions
VERSION?=v1
IMAGE=$(REGISTRY)/$(REPOSITORY):$(VERSION)

kexec_image:
	docker buildx build -f Dockerfile_kexec --load --platform linux/amd64 -t $(REGISTRY)/$(REPOSITORY)/kexec:$(VERSION) .


image2disk_image:
	docker buildx build -f Dockerfile_image2disk --load --platform linux/amd64 -t $(REGISTRY)/$(REPOSITORY)/image2disk:$(VERSION) .

all: kexec_image image2disk_image
