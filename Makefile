LANGUAGES = $(basename $(notdir $(wildcard misc/po/*.po)))
PREFIX = /usr

all: build

build:
	mkdir -p out/bin
	go build -o out/bin/ab-recovery

install: translate
	install -d ${DESTDIR}${PREFIX}/share/locale
	cp -rv out/locale/* ${DESTDIR}${PREFIX}/share/locale
	install -D out/bin/ab-recovery ${DESTDIR}${PREFIX}/lib/deepin-daemon/ab-recovery
	install -D misc/11_deepin_ab_recovery ${DESTDIR}/etc/grub.d/11_deepin_ab_recovery
	install -D misc/com.deepin.ABRecovery.conf ${DESTDIR}${PREFIX}/share/dbus-1/system.d/com.deepin.ABRecovery.conf
	install -D misc/com.deepin.ABRecovery.service ${DESTDIR}${PREFIX}/share/dbus-1/system-services/com.deepin.ABRecovery.service

clean:
	rm -rf out

out/locale/%/LC_MESSAGES/deepin-ab-recovery.mo: misc/po/%.po
	mkdir -p $(@D)
	msgfmt -o $@ $<

translate: $(addsuffix /LC_MESSAGES/deepin-ab-recovery.mo, $(addprefix out/locale/, ${LANGUAGES}))

check_code_quality:
	go vet .
