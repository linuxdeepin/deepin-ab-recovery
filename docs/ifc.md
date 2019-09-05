# 接口说明

服务类型： system bus

服务名称：com.deepin.ABRecovery

对象路径: /com/deepin/ABRecovery

接口名：com.deepin.ABRecovery

## 属性

BackingUp bool 是否正在备份

Restoring bool 是否正在恢复

ConfigValid bool 是否配置文件正确无误

## 方法

CanBackup() -> (bool)

能否备份


CanRestore() -> (bool)

能否恢复

StartBackup() -> ()

开始备份

StartRestore() -> ()

开始恢复

## 信号

JobEnd(kind string,success bool, errMsg string)

在备份或恢复任务结束发出。

kind 在备份时为 "backup"，在恢复时为 "restore"。

success 是否成功

errMsg 失败时的错误消息
