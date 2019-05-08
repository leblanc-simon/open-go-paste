#!/usr/bin/env bash

echo "package main" > ../server/fake-trans.go
echo "func FakeTrans () {" >> ../server/fake-trans.go

cat ../server/templates/views/*.gohtml \
    | grep -Ee '\{\{ *trans "([^"]+)" *\}\}' \
    | sed 's/{{/\n/g' \
    | sed "s/\(.*\)\(trans \"[^\"]\+\"\)\(.*\)/\2/" \
    | grep -E '^trans "([^"]+)"' \
    | sed 's/trans /    locale.t(/' \
    | sed 's/"$/"\)/' \
    >> ../server/fake-trans.go

cat ../server/templates/*.gohtml \
    | grep -Ee '\{\{ *trans "([^"]+)" *\}\}' \
    | sed 's/{{/\n/g' \
    | sed "s/\(.*\)\(trans \"[^\"]\+\"\)\(.*\)/\2/" \
    | grep -E '^trans "([^"]+)"' \
    | sed 's/trans /    locale.t(/' \
    | sed 's/"$/"\)/' \
    >> ../server/fake-trans.go

echo "}" >> ../server/fake-trans.go

cd ../server
go generate

rm fake-trans.go

sed -i 's/package main/package catalog/' catalog/catalog.go
