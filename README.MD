## Volcengine Command Line Tools
###  概述
1. 火山引擎-命令行工具
2. Go版本最低1.5+ 推荐是用1.12+

### 安装 volcengine-cli 

#### 通过release获取客户端
1. https://github.com/volcengine/volcengine-cli/releases 获取最新版本
2. 下载对应操作系统的版本 解压使用

#### 自行编译获取客户端

1. 使用build.sh编译客户端
    ```shell
    #如果是mac 
    sh build.sh darwain
    mv volcengine-cli-darwin volcengine-cli
    #如果是windows
    sh build.sh windows
    mv volcengine-cli-windows volcengine-cli
    #如果是linux
    sh build.sh linux
    mv volcengine-cli-linux volcengine-cli
    ```


    

### 配置 volcengine-cli

将 volcengine-cli 配置进环境变量

    检查一下 $PATH 系统变量是否存在 /usr/local/bin，若没有则请您根据实际情况为 volcengine-cli 设置可用的环境变量

    执行下面的命令，将 volcengine-cli 拷贝至 /usr/local/bin 目录下即可使用

    ```shell
    sudo cp volcengine-cli /usr/local/bin
    ```

#### 配置凭证

调用服务需要用到 AK，SK，region，有以下两种方式指定
1. 通过 `volcengine-cli configure set` 设置，示例

   ```shell
   volcengine-cli configure set --profile test --region cn-beijing --access-key ak --secret-key sk --endpoint endpoint
   ```
   支持参数及相关说明
   ```shell
   profile 配置名称 如果 profile 已存在，则该命令会修改已有配置；否则就会新建配置，并将当前使用的profile指定为新配置
   access-key 您的 AK
   secret-key 您的 SK
   region  地域，如 cn-beijing
   session-token 如果使用角色扮演，需要提供
   disable-ssl 是否禁用 ssl，默认为 false
   ```

2. 若配置文件中无配置，则会尝试从 export 的环境变量中读取配置
   ```shell
   export VOLCENGINE_ACCESS_KEY=AK
   export VOLCENGINE_SECRET_KEY=SK
   export VOLCENGINE_REGION=cn-bejing
   
   # 是否禁用SSL, 不设置的话默认为false
   export VOLCENGINE_SESSION_TOKEN=false
   
   # 如果使用角色扮演，需要提供
   export VOLCENGINE_SESSION_TOKEN=sessionToken
   ```



#### configure命令的其他操作

###### 获取指定配置信息

```shell
volcengine-cli configure get --profile [配置名]
```

其中 profile 为可选字段，若不指定 profile 字段则会展示当前配置；若指定了 profile 则 volcengine-cli 会尝试获取指定配置并展示

若配置不存在，则返回的配置字段全为默认值



###### 显示当前所有配置信息

```shell
volcengine-cli configure list
```



###### 新建/修改配置

```shell
volcengine-cli configure set --profile [配置名] --region [地区] --access-key [用户的AK] --secret-key [用户的SK] --endpoint [地区对应的endpoint]
```

新建配置时必须指定 profile 字段和 region 字段，修改已有配置必须指定 profile 字段

其余可指定的字段：

- access-key
- secret-key
- region
- session-token
- disable-ssl

各个字段的作用您可以参考上节中 "支持参数及相关说明" 部分



###### 删除配置

```shell
volcengine-cli configure delete --profile [配置名]
```

删除配置时必须指定 profile 字段以指示需要删除的配置名

若待删除的是当前正在使用的配置，则删除成功后 volcengine-cli 会尝试从剩余的配置中随机挑选一个作为当前配置




### 使用 volcengine-cli
###### 查询支持的服务列表及服务下支持的接口列表

1. 查询支持的服务列表
   ```shell
   volcengine-cli
   ```
2. 查询服务下支持的接口列表
   ```shell
   volcengine-cli ecs
   ```



###### 调用API

您可以在 https://www.volcengine.com/docs 找到想要调用的API的参数列表、返回结果等信息

使用 volcengine-cli 调用 API 时，基本命令结构如下：

```shell
volcengine-cli <service name> <action> [--parameter1 value1 --parameter2 value2 ...]
```

代码示例如下：

- 基本使用

    ```shell
    volcengine-cli ecs DescribeInstances
    ```

    ```shell
    volcengine-cli rds_mysql ListDBInstanceIPLists --InstanceId "xxxxxx"
    ```

- 支持传入 JSON

    ```shell
    volcengine-cli rds_mysql ModifyDBInstanceIPList --InstanceId "xxxxxx" --GroupName "xxxxxx" --IPList '["10.20.30.40", "50.60.70.80"]'
    ```

- 对于 ContentType 为 application/json 的请求，volcengine-cli 还支持直接将 body 作为 JSON 数据传入

    ```shell
    volcengine-cli rds_mysql ModifyDBInstanceIPList --body '{"InstanceId":"xxxxxx", "GroupName": "xxxxxx", "IPList": ["10.20.30.40", "50.60.70.80"]}'
    ```

    





