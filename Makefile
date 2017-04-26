TARGET   := minssh
VERSION  := v0.1.0
GOX_OS   := linux darwin windows
GOX_ARCH := 386 amd64
DATE     := $(shell date +%FT%T%z)
RM       := rm -f
RM_R     := rm -rf
ifeq ($(OS),Windows_NT)
	TARGET := $(TARGET).exe
endif

SHA256 := $(shell sha256sum --help)
ifdef SHA256 # usual linux
	SHA256 := sha256sum
else # macOS
	SHA256 := shasum
endif

PKG_NAME := minssh
PLATFORMS := $(foreach arch,$(GOX_ARCH),$(addsuffix _$(arch),$(GOX_OS)))
PLATFORMS_DIR := $(addprefix build/platforms/,$(PLATFORMS))
RELEASE_PREFIX := build/release/$(VERSION)/$(PKG_NAME)_$(VERSION)_

OBJECTS := $(addprefix $(RELEASE_PREFIX),$(addsuffix .tar.gz,$(filter-out windows%,$(PLATFORMS))))
OBJECTS += $(addprefix $(RELEASE_PREFIX),$(addsuffix .zip,$(filter windows%,$(PLATFORMS))))
OBJECTS += $(RELEASE_PREFIX)checksums.txt

$(TARGET):
	go build -ldflags \
		"-X main.commitHash=$$(git rev-parse --short HEAD 2>/dev/null) \
		 -X main.buildDate=$(DATE) \
		 -w -s"

build/platforms:
	mkdir -p build/platforms

$(PLATFORMS_DIR): build/platforms
	gox -os="$(GOX_OS)" -arch="$(GOX_ARCH)" \
		-output "build/platforms/{{.OS}}_{{.Arch}}/{{.Dir}}" \
		-ldflags "-X main.commitHash=$$(git rev-parse --short HEAD 2>/dev/null) \
			-X main.buildDate=$(DATE) \
			-w -s"

build/release/$(VERSION):
	mkdir -p build/release/$(VERSION)

$(RELEASE_PREFIX)%.tar.gz: $(PLATFORMS_DIR) build/release/$(VERSION)
	cd build/platforms/$(*F) && tar czf $(CURDIR)/$@ ./*

$(RELEASE_PREFIX)%.zip: $(PLATFORMS_DIR) build/release/$(VERSION)
	cd build/platforms/$(*F) && zip $(CURDIR)/$@ ./*

$(RELEASE_PREFIX)checksums.txt: build/release/$(VERSION)
	cd build/release/$(VERSION) && $(SHA256) *.tar.gz *.zip > $(@F)

.PHONY: release
release: $(OBJECTS)

.PHONY: clean
clean:
	$(RM) $(TARGET)
	$(RM_R) build
