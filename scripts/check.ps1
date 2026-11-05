#Requires -Version 5.1
<#
.SYNOPSIS
    Runs locally the same checks as CI. Run it before every commit:
        .\scripts\check.ps1
.NOTES
    Tool versions must match .github/workflows/ci.yml.
#>

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$golangciLint = 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0'
$govulncheck = 'golang.org/x/vuln/cmd/govulncheck@v1.9.0'
$gitleaks = 'github.com/zricethezav/gitleaks/v8@v8.30.1'

function Invoke-Step {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][scriptblock]$Command
    )
    Write-Host "==> $Name" -ForegroundColor Cyan
    & $Command
    if ($LASTEXITCODE -ne 0) {
        # ASCII only: Windows PowerShell 5.1 misreads UTF-8 scripts without a BOM.
        throw "Falhou: $Name (exit code $LASTEXITCODE)"
    }
}

Push-Location (Split-Path -Parent $PSScriptRoot)
try {
    Invoke-Step 'go.mod e go.sum organizados' { go mod tidy -diff }
    Invoke-Step 'go vet' { go vet ./... }
    Invoke-Step 'golangci-lint' { go run $golangciLint run ./... }
    Invoke-Step 'testes' { go test -count=1 ./... }
    Invoke-Step 'build (console)' { go build -trimpath -o "$env:TEMP\avanet-check.exe" ./cmd/avanet }
    Invoke-Step 'build (bandeja, sem janela)' { go build -trimpath -ldflags '-H=windowsgui' -o "$env:TEMP\avanetw-check.exe" ./cmd/avanetw }
    Invoke-Step 'vulnerabilidades (govulncheck)' { go run $govulncheck ./... }
    Invoke-Step 'segredos (gitleaks)' { go run $gitleaks dir --redact . }
    Write-Host 'Tudo certo.' -ForegroundColor Green
}
finally {
    Pop-Location
}
