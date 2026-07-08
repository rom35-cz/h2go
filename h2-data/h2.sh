#!/bin/sh
dir=$(dirname "$0")
java -cp "$dir/h2-2.4.240.jar:$H2DRIVERS:$CLASSPATH" org.h2.tools.Server -tcp -tcpPort 9092 -tcpAllowOthers -ifNotExists -baseDir $dir/data
