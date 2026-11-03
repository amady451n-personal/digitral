# Ghidra headless di Docker — tanpa instalasi di host
#
# Build : docker build -t ghidra-headless:latest .
# Pakai : docker run --rm \
#           -v <target-dir>:/data:ro \
#           -v <proj-dir>:/proj \
#           -v scripts:/scripts:ro -v out:/out \
#           ghidra-headless:latest /proj <projname> \
#           -import /data/<file.so> -overwrite \
#           -scriptPath "/scripts" \
#           -postScript DecompileByPrefix.java <prefix> /out/decomp.c
#
# Catatan: Ghidra 12.2 (latest saat commit) butuh JDK 25 (temurin 25-jdk-noble).
# JDK 21/24 ditolak oleh launch.sh ("unsupported java version").
FROM eclipse-temurin:25-jdk-noble

RUN apt-get update && apt-get install -y --no-install-recommends \
      wget ca-certificates unzip && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    URL=$(wget -qO- https://api.github.com/repos/NationalSecurityAgency/ghidra/releases/latest \
      | grep -oE 'https://[^"]*ghidra_[0-9.]+_PUBLIC_[0-9]+\.zip' | head -1); \
    echo "asset: $URL"; \
    wget -q "$URL" -O /tmp/ghidra.zip; \
    unzip -q /tmp/ghidra.zip -d /opt; \
    rm /tmp/ghidra.zip; \
    ln -s /opt/ghidra_* /opt/ghidra

ENTRYPOINT ["/opt/ghidra/support/analyzeHeadless"]
