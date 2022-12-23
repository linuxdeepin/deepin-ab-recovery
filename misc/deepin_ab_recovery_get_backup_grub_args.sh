#!/bin/sh
if test -e "/etc/deepin/ab-recovery.json"; then
  backup_uuid=$(jq -r '.Backup' /etc/deepin/ab-recovery.json)
  backup_dev=$(blkid -U ${backup_uuid})
  mount_dir=$(mktemp -d)
  mount ${backup_dev} ${mount_dir}

  sys_dir="${mount_dir}/etc"
  if test -f ${sys_dir}/default/grub ; then
    . ${sys_dir}/default/grub
  fi
  for x in ${sys_dir}/default/grub.d/*.cfg ; do
    if [ -e "${x}" ]; then
      . "${x}"
    fi
  done
  umount ${mount_dir}
  rm -rf ${mount_dir}
fi
if test -z "${GRUB_CMDLINE_LINUX_DEFAULT}" ; then
  DEEPIN_BACKUP_GRUB_CMDLINE_LINUX_DEFAULT="splash quiet  DEEPIN_GFXMODE=\$DEEPIN_GFXMODE ima_appraise=off checkreqprot=1  libahci.ignore_sss=1"
else
  DEEPIN_BACKUP_GRUB_CMDLINE_LINUX_DEFAULT=${GRUB_CMDLINE_LINUX_DEFAULT}
fi
echo ${DEEPIN_BACKUP_GRUB_CMDLINE_LINUX_DEFAULT}