# AppStore Architecture

End-to-end flow: how the **github-service** invokes the **app-deploy-service**
to build and register an application from a GitHub release, how the
**workflow-service** composes workflows from those App Store app references, and
how a workflow is run on an external **compute node** in a separate AWS account.

All Pennsieve services run in **Account A**; only the **compute node** that
executes the workflow lives in the external **Account B**.

```mermaid
flowchart TB
    subgraph Sources [Entry points]
      GH[GitHub release]
      UI[Pennsieve UI]
    end

    subgraph AccountA [Account A - Pennsieve]
      GHS[github-service]
      SVC[app-deploy-service]
      DDB[(DynamoDB:<br/>apps / versions / access / deployments)]
      ECS[ECS Fargate build]
      ECR[(Private App Store ECR)]
      EVB[EventBridge]
      ST[status lambda]
      WFS[workflow-service]
      WFDDB[(DynamoDB:<br/>workflows)]
      DSET[(Dataset)]
    end

    subgraph AccountB [Account B - external]
      CN[Compute Node]
    end

    GH -->|"#9993; release event"| GHS
    GHS -->|POST /store| SVC
    SVC --> DDB
    SVC --> ECS
    ECS -->|Kaniko build + push| ECR
    ECS --> EVB --> ST --> DDB

    UI -->|"list App Store apps"| SVC
    UI -->|"POST workflow (DAG of apps)"| WFS
    WFS -->|store workflows| WFDDB
    WFS -->|invoke workflow| CN
    CN -->|GET /store/registry| SVC
    SVC -->|authorized image URL| CN
    CN -->|cross-account pull| ECR
    CN --> DSET
```
</content>
