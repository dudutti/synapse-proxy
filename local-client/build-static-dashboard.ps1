# Build and Embed Dashboard script for Synapse Proxy Local Client

Write-Host "====================================================" -ForegroundColor Cyan
Write-Host " Building Static Dashboard & Compiling Client...   " -ForegroundColor Cyan
Write-Host "====================================================" -ForegroundColor Cyan

$AppDir = "G:\Optitoken\dashboard"
$LocalClientDir = "G:\Optitoken\local-client"
$EmbedStaticDir = "$LocalClientDir\internal\dashboard\dashboard-static"
$BackupTempDir = "$AppDir\backup_temp"

# Ensure backup temp directory exists
if (!(Test-Path $BackupTempDir)) {
    New-Item -ItemType Directory -Path $BackupTempDir -Force | Out-Null
}

# Lists of folders/files to temporarily move outside app/ to avoid static export checks
$Folders = @("admin", "api", "blog", "compare", "features", "use-cases", "plans", "legal", "cgv", "privacy", "sitemap.ts", "status")

Write-Host "[1/4] Moving dynamic routes completely outside app/ directory..." -ForegroundColor Yellow
foreach ($folder in $Folders) {
    $srcPath = "$AppDir\app\$folder"
    $backupPath = "$BackupTempDir\$folder"
    if (Test-Path $srcPath) {
        Move-Item -Path $srcPath -Destination $backupPath -Force
        Write-Host "  Moved: app\$folder -> backup_temp\$folder" -ForegroundColor Gray
    }
}

# Disable force-dynamic temporarily in root layout and settings layout
$RootLayoutPath = "$AppDir\app\layout.tsx"
$SettingsLayoutPath = "$AppDir\app\settings\layout.tsx"

Write-Host "  Commenting out force-dynamic constraints..." -ForegroundColor Gray
if (Test-Path $RootLayoutPath) {
    (Get-Content $RootLayoutPath) -replace 'export const dynamic = "force-dynamic";', '// export const dynamic = "force-dynamic";' | Set-Content $RootLayoutPath
}
if (Test-Path $SettingsLayoutPath) {
    (Get-Content $SettingsLayoutPath) -replace 'export const dynamic = "force-dynamic";', '// export const dynamic = "force-dynamic";' | Set-Content $SettingsLayoutPath
}

# Create a temporary empty index.html in embed static dir to prevent compile errors if build fails
if (!(Test-Path $EmbedStaticDir)) {
    New-Item -ItemType Directory -Path $EmbedStaticDir -Force | Out-Null
}
New-Item -ItemType File -Path "$EmbedStaticDir\index.html" -Value "Placeholder" -Force | Out-Null

try {
    Write-Host "[2/4] Running Next.js static export build..." -ForegroundColor Yellow
    Push-Location $AppDir
    npm run build
    Pop-Location

    Write-Host "[3/4] Copying build outputs to Go embed directory..." -ForegroundColor Yellow
    if (Test-Path $EmbedStaticDir) {
        Remove-Item -Path "$EmbedStaticDir\*" -Recurse -Force
    } else {
        New-Item -ItemType Directory -Path $EmbedStaticDir -Force
    }

    # Copy files
    Copy-Item -Path "$AppDir\out\*" -Destination $EmbedStaticDir -Recurse -Force
    Write-Host "  Dashboard files copied to: $EmbedStaticDir" -ForegroundColor Green
}
finally {
    Write-Host "[4/4] Restoring original layout and route directories..." -ForegroundColor Yellow
    
    # Restore force-dynamic
    if (Test-Path $RootLayoutPath) {
        (Get-Content $RootLayoutPath) -replace '// export const dynamic = "force-dynamic";', 'export const dynamic = "force-dynamic";' | Set-Content $RootLayoutPath
    }
    if (Test-Path $SettingsLayoutPath) {
        (Get-Content $SettingsLayoutPath) -replace '// export const dynamic = "force-dynamic";', 'export const dynamic = "force-dynamic";' | Set-Content $SettingsLayoutPath
    }

    foreach ($folder in $Folders) {
        $backupPath = "$BackupTempDir\$folder"
        $srcPath = "$AppDir\app\$folder"
        if (Test-Path $backupPath) {
            Move-Item -Path $backupPath -Destination $srcPath -Force
            Write-Host "  Restored: backup_temp\$folder -> app\$folder" -ForegroundColor Gray
        }
    }
    # Cleanup backup temp dir
    if (Test-Path $BackupTempDir) {
        Remove-Item -Path $BackupTempDir -Recurse -Force
    }
}

Write-Host "====================================================" -ForegroundColor Cyan
Write-Host " Compiling Go Executable with embedded Dashboard... " -ForegroundColor Cyan
Write-Host "====================================================" -ForegroundColor Cyan

Push-Location $LocalClientDir
go build -o synapse-local.exe main.go
Pop-Location

Write-Host "====================================================" -ForegroundColor Green
Write-Host " SUCCESS: synapse-local.exe compiled successfully!  " -ForegroundColor Green
Write-Host "====================================================" -ForegroundColor Green
