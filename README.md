# 部件

## grub 脚本

`11_deepin_recovery` 脚本放到文件夹 `/etc/grub.d/` 下。

## 配置文件

配置文件 `/etc/deepin/recovery.json`，json 对象如
```json
{
	"Current": "uuid1",
	"Backup": "uuid2",
}
```
Current 字段为正在使用分区的 uuid，Backup 字段为备份分区的 uuid。


## main.go
main.go 编译出的程序支持两个命令:

backup 备份

会把使用 rsync 工具把当前 / 的文件复制到备份分区中，然后更新grub配置文件，在已有grub菜单项中增加一个recovery 项。

restore 恢复

只有通过 grub 菜单的 recovery 项进入恢复模式下，才可以执行，否则报错，对掉备份和当前分区，更新grub配置文件，去除grub菜单项中的recovery 项目。

# 备注
命令 `lsblk -o NAME,UUID` 获取分区的名称和uuid。
