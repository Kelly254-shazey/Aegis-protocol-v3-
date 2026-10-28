# ==============================================================================
# Aegis Protocol v3 - Standalone Android APK Builder
# Uses Android SDK Build-Tools 36.0.0 & Java 21 directly
# ==============================================================================

$ErrorActionPreference = "Stop"

$SdkDir = "C:\Users\w\Android\Sdk"
$BuildTools = "$SdkDir\build-tools\36.0.0"
$AndroidJar = "$SdkDir\platforms\android-36\android.jar"
$JavaHome = "C:\Program Files\Java\jdk-21"
$Javac = "$JavaHome\bin\javac.exe"
$Jar = "$JavaHome\bin\jar.exe"
$Keytool = "$JavaHome\bin\keytool.exe"

$Aapt2 = "$BuildTools\aapt2.exe"
$D8 = "$BuildTools\d8.bat"
$Zipalign = "$BuildTools\zipalign.exe"
$ApkSigner = "$BuildTools\apksigner.bat"

Write-Host "==================================================================" -ForegroundColor Cyan
Write-Host "🛡️  BUILDING AEGIS PROTOCOL v3 NATIVE ANDROID APK" -ForegroundColor Cyan
Write-Host "==================================================================" -ForegroundColor Cyan

$AppDir = $PSScriptRoot
$BuildDir = Join-Path $AppDir "build"
if (Test-Path $BuildDir) { Remove-Item -Recurse -Force $BuildDir }
New-Item -ItemType Directory -Force -Path "$BuildDir\compiled_res", "$BuildDir\gen", "$BuildDir\classes", "$BuildDir\dex" | Out-Null

# 1. Compile Resources with aapt2
Write-Host "[1/6] Compiling Android resources with aapt2..." -ForegroundColor Yellow
& $Aapt2 compile --dir (Join-Path $AppDir "res") -o "$BuildDir\compiled_res\res.zip"

# 2. Link Resources & Generate R.java
Write-Host "[2/6] Linking resources & generating R.java..." -ForegroundColor Yellow
& $Aapt2 link -o "$BuildDir\unaligned.apk" `
    -I $AndroidJar `
    --manifest (Join-Path $AppDir "AndroidManifest.xml") `
    --java "$BuildDir\gen" `
    --auto-add-overlay `
    "$BuildDir\compiled_res\res.zip"

# 3. Compile Java Sources
Write-Host "[3/6] Compiling Java sources with javac..." -ForegroundColor Yellow
$Sources = Get-ChildItem -Recurse (Join-Path $AppDir "src\*.java"), "$BuildDir\gen\*.java" | ForEach-Object { $_.FullName }
& $Javac -encoding UTF-8 -cp $AndroidJar -d "$BuildDir\classes" $Sources

# 4. Dex bytecode with D8
Write-Host "[4/6] Converting bytecode to Dalvik Executable (classes.dex) with d8..." -ForegroundColor Yellow
$ClassFiles = Get-ChildItem -Recurse "$BuildDir\classes\*.class" | ForEach-Object { $_.FullName }
& $D8 --output "$BuildDir\dex" `
    --min-api 26 `
    --lib $AndroidJar `
    $ClassFiles

# 5. Assemble APK
Write-Host "[5/6] Assembling APK and staging native JNI libraries..." -ForegroundColor Yellow
& $Jar uf "$BuildDir\unaligned.apk" -C "$BuildDir\dex" classes.dex

# Check if native libaegis.so exists
$JniDir = Join-Path $AppDir "libs\jni"
if (Test-Path $JniDir) {
    $NativeStaging = "$BuildDir\native_staging"
    New-Item -ItemType Directory -Force -Path "$NativeStaging\lib" | Out-Null
    Copy-Item -Recurse -Force "$JniDir\*" "$NativeStaging\lib"
    & $Jar uf "$BuildDir\unaligned.apk" -C "$NativeStaging" lib
}

# 6. ZipAlign & Sign
Write-Host "[6/6] ZipAligning & signing APK..." -ForegroundColor Yellow
& $Zipalign -f -v -p 4 "$BuildDir\unaligned.apk" "$BuildDir\aligned.apk" | Out-Null

$Keystore = Join-Path $AppDir "debug.keystore"
if (-not (Test-Path $Keystore)) {
    Write-Host "Creating debug signing keystore..." -ForegroundColor Cyan
    & $Keytool -genkey -v -keystore $Keystore -storepass android -keypass android -alias androiddebugkey -keyalg RSA -keysize 2048 -validity 10000 -dname "CN=AegisProtocolV3,OU=Mobile,O=Aegis,C=US"
}

$FinalApk = Join-Path $BuildDir "AegisV3.apk"
& $ApkSigner sign --ks $Keystore --ks-pass pass:android --key-pass pass:android --out $FinalApk "$BuildDir\aligned.apk"

# Verify signature
& $ApkSigner verify -v $FinalApk

$ApkInfo = Get-Item $FinalApk
Write-Host "==================================================================" -ForegroundColor Green
Write-Host "🎉 AEGIS v3 ANDROID APK BUILT SUCCESSFULLY!" -ForegroundColor Green
Write-Host "File: $($ApkInfo.FullName)" -ForegroundColor Green
Write-Host "Size: $([math]::Round($ApkInfo.Length / 1MB, 2)) MB" -ForegroundColor Green
Write-Host "==================================================================" -ForegroundColor Green
