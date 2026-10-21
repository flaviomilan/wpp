#!/usr/bin/env bash
set -euo pipefail

# Portable installation: nothing is written to system directories.
model_name="${1:-large-v3-turbo}"
install_dir="${2:-.tools/audio}"
case "$model_name" in large-v3|large-v3-turbo|medium) ;; *) echo "Use large-v3, large-v3-turbo, or medium." >&2; exit 1;; esac
for executable in curl tar make g++ python3; do
  command -v "$executable" >/dev/null || { echo "Missing dependency: $executable" >&2; exit 1; }
done
mkdir -p "$install_dir"
install_dir="$(cd "$install_dir" && pwd)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

if command -v cmake >/dev/null; then
  cmake_binary="$(command -v cmake)"
else
  case "$(uname -m)" in x86_64) arch=x86_64;; aarch64) arch=aarch64;; *) echo "Install CMake for this architecture." >&2; exit 1;; esac
  if [[ "$(uname -s)" != Linux ]]; then echo "Install CMake before running this script outside Linux." >&2; exit 1; fi
  if [[ ! -x "$install_dir/cmake/bin/cmake" ]]; then
    curl -fL --retry 2 "https://github.com/Kitware/CMake/releases/download/v4.1.2/cmake-4.1.2-linux-$arch.tar.gz" -o "$work_dir/cmake.tar.gz"
    mkdir -p "$install_dir/cmake"
    tar -xzf "$work_dir/cmake.tar.gz" --strip-components=1 -C "$install_dir/cmake"
  fi
  cmake_binary="$install_dir/cmake/bin/cmake"
fi

whisper_version="1.8.7"
if [[ ! -f "$install_dir/whisper/.wpp-version" ]] || [[ "$(cat "$install_dir/whisper/.wpp-version")" != "$whisper_version" ]]; then
  curl -fL --retry 2 "https://codeload.github.com/ggml-org/whisper.cpp/tar.gz/refs/tags/v$whisper_version" -o "$work_dir/whisper.tar.gz"
  if [[ -e "$install_dir/whisper" ]]; then echo "Use another directory to upgrade Whisper." >&2; exit 1; fi
  mkdir -p "$install_dir/whisper"
  tar -xzf "$work_dir/whisper.tar.gz" --strip-components=1 -C "$install_dir/whisper"
  printf '%s\n' "$whisper_version" > "$install_dir/whisper/.wpp-version"
fi
"$cmake_binary" -S "$install_dir/whisper" -B "$install_dir/whisper/build" -DCMAKE_BUILD_TYPE=Release -DGGML_NATIVE=OFF
"$cmake_binary" --build "$install_dir/whisper/build" --config Release --target whisper-cli -j 4

mkdir -p "$install_dir/models"
model_file="$install_dir/models/ggml-$model_name.bin"
if [[ ! -f "$model_file" ]]; then
  curl -fL --retry 2 "https://huggingface.co/api/models/ggerganov/whisper.cpp/tree/main" -o "$work_dir/models.json"
  python3 - "$work_dir/models.json" "$model_file" <<'PY'
import json, shutil, sys
filename=sys.argv[2].rsplit("/",1)[-1]
entry=next((item for item in json.load(open(sys.argv[1])) if item["path"]==filename), None)
if not entry: raise SystemExit("Model not found in the official repository.")
if shutil.disk_usage(sys.argv[2].rsplit("/",1)[0]).free < entry["size"]+512*1024*1024:
    raise SystemExit("Insufficient space for the model and 512 MiB reserve.")
print("Model:",filename,"·",round(entry["size"]/1024**3,2),"GiB")
PY
  curl -fL --retry 2 "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-$model_name.bin" -o "$model_file.part"
  python3 - "$work_dir/models.json" "$model_file.part" "$model_file" <<'PY'
import hashlib, json, os, sys
entry=next(item for item in json.load(open(sys.argv[1])) if item["path"]==sys.argv[3].rsplit("/",1)[-1])
with open(sys.argv[2],"rb") as audio_model: digest=hashlib.file_digest(audio_model,"sha256").hexdigest()
expected=entry.get("lfs",{}).get("oid")
if expected and digest!=expected: raise SystemExit("Model hash mismatch; download not published.")
if os.path.getsize(sys.argv[2])!=entry["size"]: raise SystemExit("Model size mismatch.")
os.replace(sys.argv[2],sys.argv[3])
with open(sys.argv[3]+".sha256","w") as checksum: checksum.write(digest+"\n")
PY
fi
printf '\nReady. Start capture with:\n./wpp --whisper-bin %q --whisper-model %q run\n' "$install_dir/whisper/build/bin/whisper-cli" "$model_file"
