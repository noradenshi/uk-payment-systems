param(
    [string]$ChapsUrl = "http://localhost:8420",
    [string]$FpsUrl = "http://localhost:8421",
    [string]$BacsUrl = "http://localhost:8422",
    [switch]$ResetDockerVolumes
)

$ErrorActionPreference = "Stop"
$script:Passed = 0
$script:Failed = 0

function Write-Pass($Message) {
    $script:Passed += 1
    Write-Host "  PASS $Message" -ForegroundColor Green
}

function Write-Fail($Message) {
    $script:Failed += 1
    Write-Host "  FAIL $Message" -ForegroundColor Red
}

function New-TestId {
    return ([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString() + (Get-Random -Minimum 100 -Maximum 999))
}

function New-TestBic([string]$Prefix) {
    return ($Prefix + (Get-Random -Minimum 100000 -Maximum 999999)).ToUpperInvariant()
}

function New-TestSortCode {
    return "{0:00}-{1:00}-{2:00}" -f (Get-Random -Minimum 10 -Maximum 99), (Get-Random -Minimum 10 -Maximum 99), (Get-Random -Minimum 10 -Maximum 99)
}

function Invoke-UkpsRequest {
    param(
        [string]$Method,
        [string]$Uri,
        [object]$Body = $null,
        [string]$Token = "",
        [string]$ContentType = "application/json"
    )

    $headers = @{}
    if ($Token -ne "") {
        $headers["Authorization"] = "Bearer $Token"
    }

    $bodyText = $null
    if ($null -ne $Body) {
        if ($Body -is [string]) {
            $bodyText = $Body
        } else {
            $bodyText = $Body | ConvertTo-Json -Depth 8
        }
    }

    try {
        $response = Invoke-WebRequest -UseBasicParsing -Method $Method -Uri $Uri -Headers $headers -ContentType $ContentType -Body $bodyText
        return [pscustomobject]@{
            StatusCode = [int]$response.StatusCode
            Content = [string]$response.Content
        }
    } catch {
        $status = 0
        $content = $_.Exception.Message
        if ($_.Exception.Response) {
            $status = [int]$_.Exception.Response.StatusCode
            $stream = $_.Exception.Response.GetResponseStream()
            if ($stream) {
                $reader = New-Object System.IO.StreamReader($stream)
                $content = $reader.ReadToEnd()
            }
        }
        return [pscustomobject]@{
            StatusCode = $status
            Content = [string]$content
        }
    }
}

function Assert-Status {
    param(
        [string]$Name,
        [object]$Response,
        [int[]]$Expected
    )

    if ($Expected -contains $Response.StatusCode) {
        Write-Pass "$Name -> HTTP $($Response.StatusCode)"
    } else {
        Write-Fail "$Name -> HTTP $($Response.StatusCode), expected $($Expected -join '/'). Body: $($Response.Content.Substring(0, [Math]::Min(240, $Response.Content.Length)))"
    }
}

function Convert-JsonResponse($Response) {
    if ([string]::IsNullOrWhiteSpace($Response.Content)) {
        return $null
    }
    return $Response.Content | ConvertFrom-Json
}

function Reset-ComposeIfRequested {
    if (-not $ResetDockerVolumes) {
        return
    }

    $root = Resolve-Path (Join-Path $PSScriptRoot "..")
    Write-Host "Resetting Docker Compose volumes and rebuilding services..." -ForegroundColor Yellow
    Push-Location $root
    try {
        docker compose down -v
        docker compose up -d --build
    } finally {
        Pop-Location
    }
}

function Test-ApiKey {
    param([string]$Service, [string]$Token)

    if ([string]::IsNullOrWhiteSpace($Token)) {
        Write-Fail "$Service workflow blocked: registration did not return api_key"
        return $false
    }
    return $true
}

function Register-ChapsBank {
    param([string]$BaseUrl, [string]$Bic, [string]$Name, [string]$SortCode, [double]$Balance)

    $response = Invoke-UkpsRequest POST "$BaseUrl/v1/participants/register" @{
        bic = $Bic
        name = $Name
        sort_code = $SortCode
        balance = $Balance
    }
    Assert-Status "CHAPS register $Bic" $response @(201)
    return (Convert-JsonResponse $response).api_key
}

function Register-FpsBank {
    param([string]$BaseUrl, [string]$Bic, [string]$Name, [string]$SortCode, [double]$Balance)

    $response = Invoke-UkpsRequest POST "$BaseUrl/v1/participants/register" @{
        bic = $Bic
        name = $Name
        sort_code = $SortCode
        balance = $Balance
        participant_type = "DIRECT"
    }
    Assert-Status "FPS register $Bic" $response @(201)
    return (Convert-JsonResponse $response).api_key
}

function Register-BacsBank {
    param([string]$BaseUrl, [string]$Bic, [string]$Name, [string]$SortCode, [string]$SuCode, [double]$Balance)

    $response = Invoke-UkpsRequest POST "$BaseUrl/v1/participants/register" @{
        bic = $Bic
        name = $Name
        sort_code = $SortCode
        su_code = $SuCode
        balance = $Balance
        is_service_user = $true
        is_destination_user = $true
    }
    Assert-Status "BACS register $Bic" $response @(201)
    return (Convert-JsonResponse $response).api_key
}

function New-Std18Line([string]$Value) {
    if ($Value.Length -gt 80) {
        return $Value.Substring(0, 80)
    }
    return $Value.PadRight(80, " ")
}

function New-Standard18File {
    param(
        [string]$DestinationSortCode,
        [string]$DestinationAccount,
        [string]$Originator,
        [string]$SuCode,
        [int]$AmountPence,
        [string]$Reference
    )

    $amount = "{0:00000000000}" -f $AmountPence
    $header = New-Std18Line ("1" + "0000001" + $DestinationSortCode.PadRight(9) + $DestinationAccount.PadRight(9) + (" " * 29) + $amount + "0000001" + "260610")
    $credit = New-Std18Line ("4" + "0000001" + $DestinationSortCode.PadRight(9) + $DestinationAccount.PadRight(9) + $amount + $Originator.PadRight(15) + $Reference.PadRight(14) + $SuCode.PadRight(13))
    $trailer = New-Std18Line ("5" + "0000001" + (" " * 40) + "00000001")
    $userTrailer = New-Std18Line ("9" + "0000001" + (" " * 12) + $amount + "000000001" + "00000000000001")
    return ($header, $credit, $trailer, $userTrailer) -join "`n"
}

function Test-ChapsWorkflow {
    Write-Host ""
    Write-Host "=== CHAPS bank workflow ===" -ForegroundColor Cyan

    Assert-Status "CHAPS health" (Invoke-UkpsRequest GET "$ChapsUrl/v1/healthz") @(200)

    $id = New-TestId
    $bank1Bic = New-TestBic "CA"
    $bank2Bic = New-TestBic "CB"
    $bank1Sort = New-TestSortCode
    $bank2Sort = New-TestSortCode

    $token1 = Register-ChapsBank $ChapsUrl $bank1Bic "Integration CHAPS Bank $id A" $bank1Sort 1000000
    if (-not (Test-ApiKey "CHAPS first bank" $token1)) {
        return
    }

    $seedPayment = Invoke-UkpsRequest POST "$ChapsUrl/v1/payments/chaps" @{
        msg_id = "CHAPS-SEED-$id"
        end_to_end_id = "E2E-SEED-$id"
        receiver_bic = "HSBCGB44"
        receiver_sort_code = "40-00-00"
        amount = 1000.00
    } $token1
    Assert-Status "CHAPS payment to seeded HSBC" $seedPayment @(200, 202)

    $token2 = Register-ChapsBank $ChapsUrl $bank2Bic "Integration CHAPS Bank $id B" $bank2Sort 500000
    if (-not (Test-ApiKey "CHAPS second bank" $token2)) {
        return
    }

    $newBankPayment = Invoke-UkpsRequest POST "$ChapsUrl/v1/payments/chaps" @{
        msg_id = "CHAPS-BANK-$id"
        end_to_end_id = "E2E-BANK-$id"
        receiver_bic = $bank2Bic
        receiver_sort_code = $bank2Sort
        amount = 125.50
    } $token1
    Assert-Status "CHAPS payment to newly registered bank account" $newBankPayment @(200, 202)

    Assert-Status "CHAPS rejects missing auth" (Invoke-UkpsRequest POST "$ChapsUrl/v1/payments/chaps" @{
        msg_id = "CHAPS-NOAUTH-$id"
        receiver_bic = "HSBCGB44"
        receiver_sort_code = "40-00-00"
        amount = 10
    }) @(401)

    Assert-Status "CHAPS rejects negative amount" (Invoke-UkpsRequest POST "$ChapsUrl/v1/payments/chaps" @{
        msg_id = "CHAPS-BADAMT-$id"
        receiver_bic = "HSBCGB44"
        receiver_sort_code = "40-00-00"
        amount = -1
    } $token1) @(400)

    Assert-Status "CHAPS rejects missing receiver sort code" (Invoke-UkpsRequest POST "$ChapsUrl/v1/payments/chaps" @{
        msg_id = "CHAPS-NOSORT-$id"
        receiver_bic = "HSBCGB44"
        amount = 10
    } $token1) @(400)

    Assert-Status "CHAPS rejects invalid registration values" (Invoke-UkpsRequest POST "$ChapsUrl/v1/participants/register" @{
        bic = "bad"
        name = "Bad CHAPS Bank"
        sort_code = ""
        balance = -10
    }) @(400)

    $null = $token2
}

function Test-FpsWorkflow {
    Write-Host ""
    Write-Host "=== FPS bank workflow ===" -ForegroundColor Cyan

    Assert-Status "FPS health" (Invoke-UkpsRequest GET "$FpsUrl/v1/healthz") @(200)

    $id = New-TestId
    $bank1Bic = New-TestBic "FA"
    $bank2Bic = New-TestBic "FB"
    $bank1Sort = New-TestSortCode
    $bank2Sort = New-TestSortCode

    $token1 = Register-FpsBank $FpsUrl $bank1Bic "Integration FPS Bank $id A" $bank1Sort 500000
    if (-not (Test-ApiKey "FPS first bank" $token1)) {
        return
    }

    $seedPayment = Invoke-UkpsRequest POST "$FpsUrl/v1/payments/fps" @{
        msg_id = "FPS-SEED-$id"
        end_to_end_id = "E2E-SEED-$id"
        receiver_bic = "HSBCGB44"
        receiver_sort_code = "40-00-00"
        amount = 25.00
    } $token1
    Assert-Status "FPS payment to seeded HSBC" $seedPayment @(200, 202)

    $token2 = Register-FpsBank $FpsUrl $bank2Bic "Integration FPS Bank $id B" $bank2Sort 250000
    if (-not (Test-ApiKey "FPS second bank" $token2)) {
        return
    }

    $newBankPayment = Invoke-UkpsRequest POST "$FpsUrl/v1/payments/fps" @{
        msg_id = "FPS-BANK-$id"
        end_to_end_id = "E2E-BANK-$id"
        receiver_bic = $bank2Bic
        receiver_sort_code = $bank2Sort
        amount = 17.45
    } $token1
    Assert-Status "FPS payment to newly registered bank account" $newBankPayment @(200, 202)

    Assert-Status "FPS rejects missing auth" (Invoke-UkpsRequest POST "$FpsUrl/v1/payments/fps" @{
        msg_id = "FPS-NOAUTH-$id"
        receiver_bic = "HSBCGB44"
        receiver_sort_code = "40-00-00"
        amount = 10
    }) @(401)

    Assert-Status "FPS rejects negative amount" (Invoke-UkpsRequest POST "$FpsUrl/v1/payments/fps" @{
        msg_id = "FPS-BADAMT-$id"
        receiver_bic = "HSBCGB44"
        receiver_sort_code = "40-00-00"
        amount = -1
    } $token1) @(400)

    Assert-Status "FPS rejects invalid participant type" (Invoke-UkpsRequest POST "$FpsUrl/v1/participants/register" @{
        bic = New-TestBic "FC"
        name = "Bad FPS Bank $id"
        sort_code = New-TestSortCode
        balance = 100
        participant_type = "UNKNOWN"
    }) @(400)

    $null = $token2
}

function Test-BacsWorkflow {
    Write-Host ""
    Write-Host "=== BACS bank workflow ===" -ForegroundColor Cyan

    Assert-Status "BACS health" (Invoke-UkpsRequest GET "$BacsUrl/v1/healthz") @(200)

    $id = New-TestId
    $bank1Bic = New-TestBic "BA"
    $bank2Bic = New-TestBic "BB"
    $bank1Sort = New-TestSortCode
    $bank2Sort = New-TestSortCode
    $bank1Su = ("SU" + $id.Substring($id.Length - 8)).Substring(0, 10)
    $bank2Su = ("SB" + $id.Substring($id.Length - 8)).Substring(0, 10)

    $token1 = Register-BacsBank $BacsUrl $bank1Bic "Integration BACS Bank $id A" $bank1Sort $bank1Su 500000
    if (-not (Test-ApiKey "BACS first bank" $token1)) {
        return
    }

    $seedFile = New-Standard18File "40-00-00" "000123456" "BACS TEST A" $bank1Su 12345 "SEED$id"
    $seedPayment = Invoke-UkpsRequest POST "$BacsUrl/v1/payments/bacs/submit?filename=seed-$id.txt" $seedFile $token1 "text/plain"
    Assert-Status "BACS Standard 18 submission to seeded HSBC sort code" $seedPayment @(201, 202)

    $token2 = Register-BacsBank $BacsUrl $bank2Bic "Integration BACS Bank $id B" $bank2Sort $bank2Su 250000
    if (-not (Test-ApiKey "BACS second bank" $token2)) {
        return
    }

    $newBankFile = New-Standard18File $bank2Sort "000654321" "BACS TEST B" $bank1Su 6789 "BANK$id"
    $newBankPayment = Invoke-UkpsRequest POST "$BacsUrl/v1/payments/bacs/submit?filename=bank-$id.txt" $newBankFile $token1 "text/plain"
    Assert-Status "BACS Standard 18 submission to newly registered bank account" $newBankPayment @(201, 202)

    Assert-Status "BACS rejects missing auth" (Invoke-UkpsRequest POST "$BacsUrl/v1/payments/bacs/submit?filename=noauth-$id.txt" $seedFile "" "text/plain") @(401)

    Assert-Status "BACS rejects malformed Standard 18" (Invoke-UkpsRequest POST "$BacsUrl/v1/payments/bacs/submit?filename=bad-$id.txt" "not-a-standard-18-file" $token1 "text/plain") @(400)

    Assert-Status "BACS rejects invalid registration values" (Invoke-UkpsRequest POST "$BacsUrl/v1/participants/register" @{
        bic = "bad"
        name = "Bad BACS Bank"
        sort_code = ""
        balance = -10
    }) @(400)

    $null = $token2
}

Write-Host "UKPS bank integration workflow tests" -ForegroundColor Cyan
Write-Host "CHAPS: $ChapsUrl"
Write-Host "FPS:   $FpsUrl"
Write-Host "BACS:  $BacsUrl"

Reset-ComposeIfRequested

Test-ChapsWorkflow
Test-FpsWorkflow
Test-BacsWorkflow

Write-Host ""
Write-Host "Results: $script:Passed passed, $script:Failed failed"
if ($script:Failed -gt 0) {
    exit 1
}
