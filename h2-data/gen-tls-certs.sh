#!/bin/sh
# Generate a throwaway self-signed certificate + PKCS12 keystore for running
# a local H2 TCP server with -tcpSSL (see h2-tls.sh). Outputs land in
# h2-data/tls/ which is git-ignored. The password is a local test fixture.
# Safe to re-run: existing artifacts are kept.
set -e
dir=$(dirname "$0")
mkdir -p "$dir/tls"

if [ ! -f "$dir/tls/keystore.p12" ]; then
  keytool -genkeypair \
    -alias h2go-test \
    -keyalg RSA -keysize 2048 \
    -validity 3650 \
    -dname "CN=localhost" \
    -ext "SAN=dns:localhost,ip:127.0.0.1" \
    -keystore "$dir/tls/keystore.p12" \
    -storetype PKCS12 \
    -storepass h2gotest \
    -noprompt
fi

if [ ! -f "$dir/tls/cert.pem" ]; then
  keytool -exportcert \
    -alias h2go-test \
    -keystore "$dir/tls/keystore.p12" \
    -storetype PKCS12 \
    -storepass h2gotest \
    -rfc -file "$dir/tls/cert.pem"
fi

# Truststore so Java clients (e.g. org.h2.tools.Shell) can verify the server:
#   java -Djavax.net.ssl.trustStore=h2-data/tls/truststore.p12 \
#        -Djavax.net.ssl.trustStorePassword=h2gotest ...
if [ ! -f "$dir/tls/truststore.p12" ]; then
  keytool -importcert \
    -file "$dir/tls/cert.pem" \
    -alias h2go-test \
    -keystore "$dir/tls/truststore.p12" \
    -storetype PKCS12 \
    -storepass h2gotest \
    -noprompt
fi

echo "ready: $dir/tls/keystore.p12, $dir/tls/cert.pem, $dir/tls/truststore.p12"
