# RunningHub 智墨视频接口

创建：`POST /openapi/v2/run/workflow/{{model}}`；查询：`POST /openapi/v2/query`。请求映射提示词到节点 `263.text`、时长到 `259.value`、画幅和分辨率到 `252`。响应读取 `taskId`、`status`、`results[].url`、`errorMessage` 和 `usage`；临时结果由宿主下载，`usage.taskCostTime` 用于运行时间计费。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内 `manifest.json` 逐字段一致；documentation 使用占位文本避免递归。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "runninghub-zhimo-video",
  "name": "RunningHub 智墨视频",
  "version": "0.1.0",
  "author": "智墨",
  "description": "通过 RunningHub Workflow 提供视频生成能力。",
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "RunningHub API Key",
        "required": true
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "runninghub-workflow-video",
        "label": "RunningHub 智墨视频",
        "capabilities": [
          "video"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://www.runninghub.ai",
        "auth": {
          "type": "bearer",
          "field": "apiKey"
        },
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": true,
            "mapping": "workflowId",
            "description": "RunningHub Workflow ID。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "node 263 text",
            "description": "视频提示词。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": true,
            "mapping": "node 259 value",
            "description": "视频时长秒数。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": true,
            "mapping": "node 252 aspect_ratio",
            "description": "视频画幅。"
          },
          {
            "name": "resolution",
            "type": "number",
            "required": true,
            "mapping": "node 252 megapixels",
            "description": "megapixels 分辨率。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/openapi/v2/run/workflow/{{model}}",
          "contentType": "application/json",
          "body": {
            "addMetadata": false,
            "nodeInfoList": [
              {
                "nodeId": "263",
                "fieldName": "text",
                "fieldValue": {
                  "$ref": "request.prompt"
                }
              },
              {
                "nodeId": "259",
                "fieldName": "value",
                "fieldValue": {
                  "$ref": "request.duration"
                }
              },
              {
                "nodeId": "252",
                "fieldName": "aspect_ratio",
                "fieldValue": {
                  "$ref": "request.aspectRatio"
                }
              },
              {
                "nodeId": "252",
                "fieldName": "megapixels",
                "fieldValue": {
                  "$ref": "request.resolution"
                }
              }
            ],
            "instanceType": "default",
            "usePersonalQueue": false
          }
        },
        "poll": {
          "method": "POST",
          "path": "/openapi/v2/query",
          "contentType": "application/json",
          "body": {
            "taskId": {
              "$ref": "taskId"
            }
          }
        },
        "response": {
          "taskId": {
            "$ref": "response.taskId"
          },
          "status": {
            "$ref": "response.status"
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.errorMessage"
              },
              {
                "$ref": "response.errorCode"
              }
            ]
          },
          "videos": {
            "$ref": "response.results"
          },
          "usage": {
            "$ref": "response.usage"
          },
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->