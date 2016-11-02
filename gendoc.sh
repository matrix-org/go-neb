#!/bin/bash

DOC_DIR=godoc

# Run a godoc server which we will scrape. Clobber the GOPATH to include
# only our dependencies.
GOPATH=$(pwd):$(pwd)/vendor godoc -http=localhost:6060 &
DOC_PID=$!

# Wait for the server to init
sleep 1

# Scrape the pkg directory for the API docs. Scrap lib for the CSS/JS. Ignore everything else.
# The output is dumped to the directory "localhost:6060".
wget -r -m -k -E -p --include-directories="/pkg,/lib" --exclude-directories="*" http://localhost:6060/pkg/github.com/matrix-org/go-neb/

# Stop the godoc server
kill -9 $DOC_PID

# Delete the old directory or else mv will put the localhost dir into
# the DOC_DIR if it already exists.
rm -rf $DOC_DIR
mv localhost\:6060 $DOC_DIR

echo "Docs can be found in $DOC_DIR"
echo "Replace /lib and /pkg in the gh-pages branch to update gh-pages"
