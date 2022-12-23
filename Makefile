LANGUAGES = $(basename $(notdir $(wildcard misc/po/*.po)))
PREFIX = /usr
ARCH = ${shell uname -m}
export GO111MODULE=off

all: build

build:
	mkdir -p out/bin
	go build -o out/bin/ab-recovery $(GO_BUILD_FLAGS)

install: translate
	install -d ${DESTDIR}${PREFIX}/share/locale
	cp -rv out/locale/* ${DESTDIR}${PREFIX}/share/locale
    ifeq (${ARCH},sw_64)
		install -D misc/11_deepin_ab_recovery ${DESTDIR}/etc/grub.d/sw64/11_deepin_ab_recovery
    else
		install -D misc/11_deepin_ab_recovery ${DESTDIR}/etc/grub.d/11_deepin_ab_recovery
    endif
	install -D out/bin/ab-recovery ${DESTDIR}${PREFIX}/lib/deepin-daemon/ab-recovery
	install -m 0644 -D misc/12_deepin_ab_recovery.cfg \
		${DESTDIR}/etc/default/grub.d/12_deepin_ab_recovery.cfg
	install -m 0644 -D misc/com.deepin.ABRecovery.conf ${DESTDIR}${PREFIX}/share/dbus-1/system.d/com.deepin.ABRecovery.conf
	install -m 0644 -D misc/com.deepin.ABRecovery.service ${DESTDIR}${PREFIX}/share/dbus-1/system-services/com.deepin.ABRecovery.service
	mkdir -p ${DESTDIR}${PREFIX}/libexec/deepin-ab-recovery
	install -D misc/deepin_ab_recovery_get_backup_grub_args.sh ${DESTDIR}${PREFIX}/libexec/deepin-ab-recovery/deepin_ab_recovery_get_backup_grub_args.sh
test:
	go test -v ./...

test-coverage:
	env GOPATH="${CURDIR}/${GOBUILD_DIR}:${GOPATH}" go test -cover -v ./... | awk '$$1 ~ "(ok|\\?)" {print $$2","$$5}' | sed "s:${CURDIR}::g" | sed 's/files\]/0\.0%/g' > coverage.csv

print_gopath:
	GOPATH="${CURDIR}/${GOPATH_DIR}:${GOPATH}"

clean:
	rm -rf out

out/locale/%/LC_MESSAGES/deepin-ab-recovery.mo: misc/po/%.po
	mkdir -p $(@D)
	msgfmt -o $@ $<

translate: $(addsuffix /LC_MESSAGES/deepin-ab-recovery.mo, $(addprefix out/locale/, ${LANGUAGES}))

check_code_quality:
	go vet .

pot:
	xgettext -kTr --language=C -o misc/po/deepin-ab-recovery.pot *.go 
