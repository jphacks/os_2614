#!/bin/bash
# rec.sh start <name> | stop <name> | shot
A=~/Library/Android/sdk/platform-tools/adb
D=/private/tmp/claude-501/-Users-ringo-Documents-docs-projects-os-2614/6c3b2414-6125-4299-a286-0b91789d2d22/scratchpad/demo
case $1 in
  start) $A shell "nohup screenrecord --bit-rate 16000000 /sdcard/$2.mp4 >/dev/null 2>&1 &"; sleep 1;;
  stop) $A shell pkill -INT screenrecord; sleep 2; $A pull /sdcard/$2.mp4 $D/raw/$2.mp4 >/dev/null && $A shell rm /sdcard/$2.mp4 && ffprobe -v error -show_entries format=duration -of csv=p=0 $D/raw/$2.mp4;;
  shot) $A exec-out screencap -p > $D/s.png; sips -Z 800 $D/s.png >/dev/null;;
esac
