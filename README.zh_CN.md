# deepin AB Recovery

A/B 备份和恢复服务

## Dependencies
请查看“debian/control”文件中提供的“Depends”。

### Build dependencies
请查看“debian/control”文件中提供的“Build-Depends”。

## 安装

确保已经安装了所有的编译依赖
```
$ sudo apt-get build-dep deepin-ab-recovery
```

构建
```
$ make
```

```
$ sudo make install
```

或者，生成包文件并在深度终端安装它
```
$ debuild -uc -us ...
$ sudo dpkg -i ../deepin-ab-recovery*.deb
```

## 用法
参考以下文档:

[接口](./docs/ifc.md)
[设计](./docs/design.md)

## 获得帮助

如果您遇到任何其他问题，您可能会发现这些渠道很有用：

* [Gitter](https://gitter.im/orgs/linuxdeepin/rooms)
* [IRC channel](https://webchat.freenode.net/?channels=deepin)
* [Forum](https://bbs.deepin.org)
* [WiKi](http://wiki.deepin.org/)

## 贡献指南

我们鼓励您报告问题并做出更改

* [Contribution guide for users](http://wiki.deepin.org/index.php?title=Contribution_Guidelines_for_Users)
* [Contribution guide for developers](http://wiki.deepin.org/index.php?title=Contribution_Guidelines_for_Developers).

## License

deepin AB Recovery 在 [GPL-3.0-or-later](LICENSE)下发布。
