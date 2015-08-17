#!/bin/bash

set -e

function str_to_array {
  eval "local input=\"\$$1\""
  input="$(echo "$input" | awk '
  {
    split($0, chars, "")
    for (i = 1; i <= length($0); i++) {
      if (i > 1) {
        printf(", ")
      }
      printf("'\''%s'\''", chars[i])
    }
  }
  ')"
  eval "$1=\"$input\""
}

function update_access_key {
  str_to_array DOMAIN
  str_to_array BUCKET
  str_to_array REMOTE_ROOT
  str_to_array ALIYUN_ACCESS_KEY
  str_to_array ALIYUN_ACCESS_SECRET
  awk "
  /DEFAULT_DOMAIN/ {
    print \"var DEFAULT_DOMAIN = []byte{${DOMAIN}}\"
    next
  }
  /DEFAULT_BUCKET/ {
    print \"var DEFAULT_BUCKET = []byte{${BUCKET}}\"
    next
  }
  /DEFAULT_ROOT/ {
    print \"var DEFAULT_ROOT = []byte{${REMOTE_ROOT}}\"
    next
  }
  /KEY/ {
    print \"var KEY = []byte{${ALIYUN_ACCESS_KEY}}\"
    next
  }
  /SECRET/ {
    print \"var SECRET = []byte{${ALIYUN_ACCESS_SECRET}}\"
    next
  }
  {
    print
  }
  " access.go > _access.go

  mv _access.go access.go
}

if test -z "$DOMAIN"; then
  echo -n "Please enter default API domain: (oss-cn-hangzhou.aliyuncs.com if empty) "
  read DOMAIN
  if test -z "$DOMAIN"; then
    DOMAIN=oss-cn-hangzhou.aliyuncs.com
  fi
fi
while test -z "$BUCKET"; do
  echo -n "Please enter default bucket name: "
  read BUCKET
done
if test -z "$REMOTE_ROOT"; then
  echo -n "Please enter default remote root directory: (can be empty) "
  read REMOTE_ROOT
fi
while test -z "$ALIYUN_ACCESS_KEY"; do
  echo -n "Please paste your access key ID: (will not be echoed) "
  read -s ALIYUN_ACCESS_KEY
  echo
done
while test -z "$ALIYUN_ACCESS_SECRET"; do
  echo -n "Please paste your access key SECRET: (will not be echoed) "
  read -s ALIYUN_ACCESS_SECRET
  echo
done
update_access_key

if test -n "$BUILD_DOCKER"; then
  docker-compose up
  docker-compose rm --force -v
else
  go build
fi

DOMAIN="oss-cn-hangzhou.aliyuncs.com"
BUCKET="bucket"
REMOTE_ROOT="oss"
ALIYUN_ACCESS_KEY="key"
ALIYUN_ACCESS_SECRET="secret"
update_access_key