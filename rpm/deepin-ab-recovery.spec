# Run tests in check section
# disable for bootstrapping
%bcond_with check

Name:           deepin-ab-recovery
Version:        0.0.5
Release:        1
Summary:        deepin AB Recovery
License:        GPLv3+
Source0:        %{name}_%{version}.orig.tar.xz

BuildRequires:  compiler(go-compiler)
BuildRequires:  pkgconfig(alsa)
BuildRequires:  pkgconfig(cairo-ft)
BuildRequires:  pkgconfig(gio-2.0)
BuildRequires:  pkgconfig(gtk+-3.0)
BuildRequires:  pkgconfig(gdk-pixbuf-xlib-2.0)
BuildRequires:  pkgconfig(gudev-1.0)
BuildRequires:  pkgconfig(libcanberra)
BuildRequires:  pkgconfig(libpulse-simple)
BuildRequires:  pkgconfig(librsvg-2.0)
BuildRequires:  pkgconfig(poppler-glib)
BuildRequires:  pkgconfig(polkit-qt5-1)
BuildRequires:  pkgconfig(systemd)
BuildRequires:  pkgconfig(xfixes)
BuildRequires:  pkgconfig(xcursor)
BuildRequires:  pkgconfig(x11)
BuildRequires:  pkgconfig(xi)
BuildRequires:  pkgconfig(gobject-introspection-1.0)
BuildRequires:  pkgconfig(gudev-1.0)
BuildRequires:  pkgconfig(sqlite3)
BuildRequires:  deepin-gettext-tools
BuildRequires:  gocode

%define debug_package %{nil}

%description
deepin AB Recovery

%prep
%autosetup

%build
export GOPATH=/usr/share/gocode
make flags=-trimpath

%install
install -d -p %{buildroot}/usr/lib/deepin-daemon
cp -pav ./out/bin/ab-recovery %{buildroot}/usr/lib/deepin-daemon
echo "/usr/lib/deepin-daemon/ab-recovery" >> devel.file-list

%files -f devel.file-list
%doc README.md
%license LICENSE

%changelog
* Tue May 28 2019 Robin Lee <cheeselee@fedoraproject.org> - 3.17.0-2
- Fix a security issue


