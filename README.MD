### Volcengine Command Line Tools
####  概述
1. 火山引擎-命令行工具
2. Go版本最低1.5+ 推荐是用1.12+

##### 使用方式
###### 设置AK SK region
调用服务需要用到 AK，SK，region，有以下两种方式指定
1. 使用./vestack-cli configure set设置，示例
   ```shell
   volcengine-cli configure set --profile test --region cn-beijing --access-key ak --secret-key sk
   ```
   支持参数和说明
   ```shell
   profile 名称 如果profile已存在，则该命令会修改已有配置；否则就会新建配置，并将当前使用的profile指定为新配置
   region  地域，如cn-beijing
   access-key 您的AK
   secret-key 您的SK
   session-token 如果使用角色扮演，需要提供
   ```

2. 若配置文件中无配置，则会尝试从export的环境变量中读取配置
   ```shell
   export VOLCENGINE_ACCESS_KEY=AK
   export VOLCENGINE_SECRET_KEY=SK
   export VOLCENGINE_REGION=cn-bejing
   #如果使用角色扮演，需要提供
   export VOLCENGINE_SESSION_TOKEN=sessionToken
   ```
###### 调用API
1. 支持的服务列表
   ```shell
   volcengine-cli
   ```
2. 服务下支持的接口列表
   ```shell
   volcengine-cli ecs
   ```