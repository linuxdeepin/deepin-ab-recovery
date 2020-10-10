# 设计

项目名称： deepin AB Recovery

为了满足需求：更新前把A系统同步到B系统，创建GRUB回退系统选项；
选择回退系统选项后，对调 A/B 系统角色；

## 分区要求

为使此工具正确工作，需要满足以下分区要求：

- 一个根分区和一个候选根分区，根分区挂载在 /，候选根分区不挂载

- 一个分区挂载在 /boot

- 一个分区挂载在 /home

## 配置文件

配置文件 `/etc/deepin/ab-recovery.json`，json 对象如
```json
{
	"Current": "uuid1",
	"Backup": "uuid2"
}
```

Current 字段为正在使用分区的 uuid，Backup 字段为备份分区的 uuid。

此配置文件应该由系统安装器负责写入。

## 还原菜单项目的生成脚本

源码位置: misc/11_deepin_ab_recovery

安装位置: /etc/grub.d/11_deepin_ab_recovery

这个脚本被 grub-mkconfig 命令执行，执行顺序需要在 10_linux 后和在 30_os-prober 之前。此脚本会读取 配置文件 /etc/default/grub.d/11_deepin_ab_recovery.cfg 中的配置。


## 备份过程

备份条件：根分区的 uuid 等于配置文件中的 Current 字段的值。

根分区指被挂载到文件夹 / 的硬盘分区。

把备份分区挂载到文件夹 /deepin-ab-recovery-backup，然后使用 rsync 命令把根分区的内容同步到备份分区，同步时忽略 /sys、/dev、/proc、/run、/media、/home、/tmp、/boot、/deepin-ab-recovery-backup。

然后修正备份分区中 etc/fstab（即恢复模式系统使用的 /etc/fstab）中 / 的 UUID 为备份分区的 uuid。

然后备份内核，在文件夹 /boot 查找正在使用的内核，复制到文件夹 /boot/deepin-ab-recovery。

把备份分区uuid，内核文件信息写入 /etc/default/grub.d/11_deepin_ab_recovery.cfg，用于帮助 grub 菜单项目中 Recovery 项目生成。同时也把备份分区的信息加入 GRUB_OS_PROBER_SKIP 中，这样在生成其他系统启动项时会跳过备份分区。

最后执行 grub-mkconfig 命令更新 grub 配置文件。

## 还原过程

还原条件：根分区的 uuid 等于配置文件中的 Backup 字段的值。

把备份分区的信息加入 GRUB_OS_PROBER_SKIP_LIST 中，然后执行 grub-mkconfig 命令更新 grub 配置文件。

## 特殊场景分析

### 使用备份还原工具恢复出厂设置

恢复出厂设置会恢复系统的根分区和 /boot 分区，导致重要配置文件 `/etc/default/grub.d/11_deepin_ab_recovery.cfg` 丢失，如果不加以拯救，就会让 `/etc/grub.d/30_os-prober` 脚本输出备份分区中的系统到启动项配置脚本文件 `/boot/grub/grub.cfg` 中，添加**多余**的启动项目。

采用的解决方案为：

增加配置脚本 `/etc/default/grub.d/12_deepin_ab_recovery.cfg` 让它在配置文件 `/etc/default/grub.d/11_deepin_ab_recovery.cfg`  丢失时，调用命令 `ab-recovery -print-sh-hide-os`，这个命令会输出类似的内容：
```
GRUB_OS_PROBER_SKIP_LIST="$GRUB_OS_PROBER_SKIP_LIST 8bafe9c6-71f5-4b5c-8923-accb280cc12b@/dev/nvme0n1p4"
```
可以让 `30_os-prober` 脚本跳过备份分区。

`ab-recovery` 的 `-print-sh-hide-os` 选项的处理逻辑主要在 printShHideOs 函数中，基本原理是调用 os-prober 命令找到所有安装有我们的 OS 的分区设备，然后判断分区中是否有备份标记文件 `.deepin-ab-recovery-backup`, 如果有则此分区是那个备份分区，就应该过滤掉，就不会让 `30_os-prober` 输出多余启动项目了。