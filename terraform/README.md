# Terraform Infrastructure

Terraform-konfiguration för att driftsätta Ortodoxa Gudstjänster på Google Cloud Platform.

## Arkitektur

```
┌──────────────────────────────────────────────────────────┐
│ Insamling (var 6:e timme)                                │
│ Cloud Scheduler → Cloud Run Job → Scraping → Firestore   │
└──────────────────────────────────────────────────────────┘
                                          ↓
┌──────────────────────────────────────────────────────────┐
│ Webbserver                                               │
│ ortodoxagudstjanster.se → Cloud Run → Läs Firestore      │
└──────────────────────────────────────────────────────────┘
```

## Resurser

| Resurs | Namn | Beskrivning |
|--------|------|-------------|
| Cloud Run Service | `ortodoxa-gudstjanster` | Webbserver |
| Cloud Run Job | `ortodoxa-gudstjanster-ingest` | Insamlingsjobb (scraping) |
| Cloud Scheduler | `ortodoxa-gudstjanster-ingest-schedule` | Triggar insamling var 6:e timme |
| Firestore | `(default)` | Lagring av gudstjänstdata |
| GCS Bucket | `ortodoxa-gudstjanster-ortodoxa-store` | Cache för Vision API-resultat |
| Artifact Registry | `ortodoxa-gudstjanster` | Docker-images |
| Secret Manager | `ortodoxa-gudstjanster-*` | Hemligheter (API-nycklar, SMTP) |

## Tjänstkonton

| Konto | Syfte |
|-------|-------|
| `ortodoxa-gudstjanster-sa` | Webbserver (läs Firestore, SMTP-hemligheter) |
| `ortodoxa-ingest-sa` | Insamlingsjobb (skriv Firestore, GCS, OpenAI-hemlighet) |
| `ortodoxa-scheduler-sa` | Cloud Scheduler (starta insamlingsjobb) |

## Filer

| Fil | Beskrivning |
|-----|-------------|
| `main.tf` | Provider-konfiguration |
| `variables.tf` | Input-variabler |
| `storage.tf` | GCS-bucket, Artifact Registry |
| `secrets.tf` | Secret Manager-resurser |
| `cloudrun.tf` | Cloud Run-tjänst, domänmappning |
| `firestore.tf` | Firestore-databas och index |
| `job.tf` | Cloud Run Job för insamling |
| `scheduler.tf` | Cloud Scheduler för periodisk insamling |
| `iam.tf` | Tjänstkonton och behörigheter |
| `outputs.tf` | Output-värden |

## Första gången

1. Skapa ett GCP-projekt och aktivera billing

2. Initiera Terraform:
   ```bash
   cd terraform
   terraform init
   ```

3. Skapa konfigurationsfil:
   ```bash
   cp terraform.tfvars.example terraform.tfvars
   ```

4. Redigera `terraform.tfvars`:
   ```hcl
   project_id = "ditt-projekt-id"
   domain     = "dindomän.se"  # valfritt
   ```

5. Driftsätt infrastrukturen:
   ```bash
   terraform apply
   ```

6. Lägg till hemligheter i Secret Manager:
   ```bash
   # OpenAI API-nyckel (krävs för insamling)
   echo -n "sk-..." | gcloud secrets versions add ortodoxa-gudstjanster-openai-api-key --data-file=-

   # SMTP-konfiguration (valfritt, för feedback-mejl)
   echo -n "smtp.gmail.com" | gcloud secrets versions add ortodoxa-gudstjanster-smtp-host --data-file=-
   echo -n "587" | gcloud secrets versions add ortodoxa-gudstjanster-smtp-port --data-file=-
   echo -n "user@gmail.com" | gcloud secrets versions add ortodoxa-gudstjanster-smtp-user --data-file=-
   echo -n "app-password" | gcloud secrets versions add ortodoxa-gudstjanster-smtp-pass --data-file=-
   echo -n "recipient@example.com" | gcloud secrets versions add ortodoxa-gudstjanster-smtp-to --data-file=-
   ```

7. Bygg och pusha Docker-image:
   ```bash
   docker build --platform linux/amd64 -t europe-north1-docker.pkg.dev/PROJEKT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest ..
   docker push europe-north1-docker.pkg.dev/PROJEKT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest
   ```

8. Driftsätt webbservern:
   ```bash
   gcloud run deploy ortodoxa-gudstjanster \
     --image=europe-north1-docker.pkg.dev/PROJEKT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest \
     --region=europe-north1
   ```

9. Kör insamlingsjobbet manuellt första gången:
   ```bash
   gcloud run jobs execute ortodoxa-gudstjanster-ingest --region=europe-north1
   ```

## Uppdatera

### Ny version av koden

```bash
# Bygg och pusha
docker build --platform linux/amd64 -t europe-north1-docker.pkg.dev/PROJEKT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest ..
docker push europe-north1-docker.pkg.dev/PROJEKT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest

# Driftsätt webbserver
gcloud run deploy ortodoxa-gudstjanster \
  --image=europe-north1-docker.pkg.dev/PROJEKT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest \
  --region=europe-north1

# Insamlingsjobbet använder automatiskt senaste imagen vid nästa körning
```

### Uppdatera infrastruktur

```bash
terraform plan   # Granska ändringar
terraform apply  # Applicera ändringar
```

## Manuell insamling

```bash
gcloud run jobs execute ortodoxa-gudstjanster-ingest --region=europe-north1
```

## Visa loggar

```bash
# Webbserver
gcloud run services logs read ortodoxa-gudstjanster --region=europe-north1 --limit=50

# Insamlingsjobb
gcloud logging read 'resource.type="cloud_run_job" resource.labels.job_name="ortodoxa-gudstjanster-ingest"' \
  --project=ortodoxa-gudstjanster --limit=50 --format="value(textPayload)"
```

## Variabler

| Variabel | Beskrivning | Standard |
|----------|-------------|----------|
| `project_id` | GCP-projekt-ID | (krävs) |
| `region` | GCP-region | `europe-north1` |
| `service_name` | Namn på tjänsten | `ortodoxa-gudstjanster` |
| `image_tag` | Docker-image-tag | `latest` |
| `domain` | Anpassad domän (valfritt) | `""` |

## Outputs

| Output | Beskrivning |
|--------|-------------|
| `service_url` | URL till Cloud Run-tjänsten |
| `artifact_registry_url` | URL till Artifact Registry |
| `storage_bucket` | Namn på GCS-bucket |
| `service_account_email` | E-post till tjänstkontot |
| `ingest_job_name` | Namn på insamlingsjobbet |
| `scheduler_job_name` | Namn på scheduler-jobbet |
| `firestore_database` | Firestore-databasnamn |
