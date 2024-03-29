# deepin-ab-recovery

A/B backup and restore service

## Dependencies
You can also check the "Depends" provided in the debian/control file.

### Build dependencies
You can also check the "Build-Depends" provided in the debian/control file.

## Installation

Install prerequisites
```
$ sudo apt-get build-dep deepin-ab-recovery
```

Build
```
$ make
```

```
$ sudo make install
```

Or, generate package files and install Deepin Terminal with it
```
$ debuild -uc -us ...
$ sudo dpkg -i ../deepin-ab-recovery*.deb
```

## Usage
refer to the following documents:

[接口](./docs/ifc.md)
[设计](./docs/design.md)

## Getting help

Any usage issues can ask for help via

* [Gitter](https://gitter.im/orgs/linuxdeepin/rooms)
* [IRC channel](https://webchat.freenode.net/?channels=deepin)
* [Forum](https://bbs.deepin.org)
* [WiKi](http://wiki.deepin.org/)

## Getting involved

We encourage you to report issues and contribute changes

* [Contribution guide for users](http://wiki.deepin.org/index.php?title=Contribution_Guidelines_for_Users)
* [Contribution guide for developers](http://wiki.deepin.org/index.php?title=Contribution_Guidelines_for_Developers).

## License

deepin-ab-recovery is licensed under [GPL-3.0-or-later](LICENSE).
