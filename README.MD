## Volcengine Command Line Tools

中文 | [English](README.EN.MD)

### 特别说明
为了便于用户使用，从v1.0.20版本开始，火山引擎CLI工具命令前缀由“volcengine-cli”  更新为“ve”。低版本不受影响，请升级到v1.0.20及以后版本的用户及时更新指令前缀（可参考alias命令统一设置）。

###  概述
1. 火山引擎-命令行工具
2. Go版本最低1.5+ 推荐是用1.12+

### 安装 火山引擎CLI 

#### 通过release获取客户端
1. https://github.com/volcengine/volcengine-cli/releases 获取最新版本
2. 下载对应操作系统的版本 解压使用

#### 自行编译获取客户端

1. 使用build.sh编译客户端
    ```shell
    # 如果是mac 
    sh build.sh darwin
    mv ve-darwin ve
    # 如果是windows
    sh build.sh windows
    mv ve-windows ve
    # 如果是linux
    sh build.sh linux
    mv ve-linux ve
    ```


​    

### 配置 火山引擎CLI

将 火山引擎CLI 配置进环境变量

1. 检查一下 $PATH 系统变量是否存在 /usr/local/bin，若没有则请您根据实际情况为 火山引擎CLI 设置可用的环境变量

2. 执行下面的命令，将 ve 拷贝至 /usr/local/bin 目录下即可使用

    ```shell
    sudo cp ve /usr/local/bin
    ```



#### 配置凭证

调用服务需要用到 AK，SK，region，可以通过以下两种方式进行配置
1. 通过 ve configure set 设置，示例

   ```shell
   ve configure set --profile test --region cn-beijing --access-key ak --secret-key sk
   ```
   支持参数及相关说明

   ```shell
   profile: 配置名称，如果 profile 已存在，则该命令会修改已有配置；否则就会新建配置，并将当前使用的profile指定为新配置
   access-key: 您的 AK
   secret-key: 您的 SK
   region: 地域，如 cn-beijing
   session-token: 如果使用角色扮演，需要提供
   disable-ssl: 是否禁用 ssl，默认为 false
   endpoint: 可以不填，默认值为 open.volcengineapi.com
   ```

2. 若配置文件中无配置，则会尝试从 export 的环境变量中读取配置
   ```shell
   export VOLCENGINE_ACCESS_KEY=AK
   export VOLCENGINE_SECRET_KEY=SK
   export VOLCENGINE_REGION=cn-bejing
   
   # 是否禁用SSL, 不设置的话默认为false
   export VOLCENGINE_DISABLE_SSL=false
   
   # 如果使用角色扮演，需要提供
   export VOLCENGINE_SESSION_TOKEN=sessionToken
   ```



#### configure命令的其他操作

1. 火山引擎CLI使用configure配置profile之后，会优先使用profile中的鉴权信息进行接口签名访问
2. 新增或者修改profile后，当前configure中的默认使用profile会被切换到新增或者修改的profile上
3. 如果存在多个profile，请在调用接口前，使用ve configure profile --profile [配置名] 进行切换，以保证正确的使用

###### 获取指定配置(profile)信息

```shell
ve configure get --profile [配置名]
```

其中 profile 为可选字段，若不指定 profile 字段则会展示当前配置；若指定了 profile 则 火山引擎CLI 会尝试获取指定配置并展示

若配置不存在，则返回的配置字段全为默认值



###### 显示当前所有配置(profile)信息

```shell
ve configure list
```

###### 切换当前使用的配置(profile)信息 请确保版本大于或者等于>=1.0.16

```shell
ve configure profile --profile [配置名]
```

其中 profile 为必选字段，指定了 profile 则 火山引擎CLI 会尝试获切换当前的配值

若配置不存在，当前的配置不会发生切换，并且会给出错误的提示


###### 新建/修改配置(profile)

```shell
ve configure set --profile [配置名] --region [地区] --access-key [用户的AK] --secret-key [用户的SK] --endpoint [地区对应的endpoint]
```

新建或者修改配置后，当前的使用配置会切换到修改的配置上

新建配置时必须指定 profile 字段和 region 字段，修改已有配置必须指定 profile 字段

其余可指定的字段：

- access-key
- secret-key
- region
- session-token
- disable-ssl

各个字段的作用您可以参考上节中 "支持参数及相关说明" 部分



###### 删除配置(profile)

```shell
ve configure delete --profile [配置名]
```

删除配置时必须指定 profile 字段以指示需要删除的配置名

若待删除的是当前正在使用的配置，则删除成功后 火山引擎CLI 会尝试从剩余的配置中随机挑选一个作为当前配置



#### 配置自动补全

使用 ve completion --help 可以查看各种终端下配置自动补全的方式，用户可以根据提示信息自己选择是否配置自动补全功能

<br/>

##### Bash

火山引擎CLI 的 Bash 补全脚本可以通过 ve completion bash 进行查看，在 shell 中导入该自动补全脚本，即可开启自动补全功能

###### 1. 安装 bash-completion

火山引擎CLI 补全脚本依赖于工具 bash-completiom，所以你必须先安装并启用它（可以用命令 type _init_completion 检查 bash-completion 是否已安装）

1. 安装：yum install bash-completion 或 apt-get install bash-completion

2. 启用 bash-completion：source /usr/share/bash-completion/bash_completion

    建议将 source /usr/share/bash-completion/bash_completion 添加至 ~/.bashrc 中

3. 检查 bash-completion 是否安装成功：执行 type _init_completion 验证 bash-completion 的安装状态



###### 2. 配置自动补全

按以下步骤配置即可在 Bash 下开启自动补全功能：

1. echo 'source <(ve completion bash)' >> ~/.bashrc
2. ve completion bash > /etc/bash_completion.d/ve

之后重新加载 Shell （或者 source ~/.bashrc） 后即可生效



若出现 _get_comp_words_by_ref: command not found 的错误，请再次检查 bash-completion 是否安装配置成功

<br/>

##### Zsh

火山引擎CLI 的 Zsh 补全脚本可以通过 ve completion zsh 进行查看，在 shell 中导入该自动补全脚本，即可开启自动补全功能

按以下步骤配置即可在 Zsh 下开启自动补全功能：

1. Zsh 中启用 compinit：
    echo "autoload -U compinit; compinit" >> ~/.zshrc
2. 配置自动补全
    ve completion zsh > "${fpath[1]}/_ve"

之后重新加载 Shell （或者 source ~/.zshrc） 后即可生效





#### 配置颜色 

使用 ve enable-color 可以开启彩色显示功能

使用 ve disable-color 可以关闭彩色显示功能




### 使用 火山引擎CLI
#### 查询支持的服务列表及服务下支持的接口列表

1. 查询支持的服务列表
   ```shell
   ve [-h|--help]
   ```
2. 查询服务下支持的接口列表
   ```shell
   ve ecs [-h|--help]
   ```



#### 调用API

使用 火山引擎CLI 调用 API 时，基本命令结构如下：

```shell
ve <service name> <action> [--parameter1 value1 --parameter2 value2 ...]
```

您可以使用 ve &lt;service name&gt; &lt;action&gt; --help 查看想要调用的API的参数列表、返回结果等信息，例如：ve ecs DescribeInstances --help

若需要查看更详细的信息，您也可以在 https://www.volcengine.com/docs 进行查阅

<br/>

以下提供一些代码示例：

- 基本使用

    ```shell
    ve ecs DescribeInstances
    ```

    ```shell
    ve rds_mysql ListDBInstanceIPLists --InstanceId "xxxxxx"
    ```

- 支持传入 JSON

    ```shell
    ve rds_mysql ModifyDBInstanceIPList --InstanceId "xxxxxx" --GroupName "xxxxxx" --IPList '["10.20.30.40", "50.60.70.80"]'
    ```

- 对于 ContentType 为 application/json 的请求，火山引擎CLI 还支持直接将 body 作为 JSON 数据传入

    ```shell
    ve rds_mysql ModifyDBInstanceIPList --body '{"InstanceId":"xxxxxx", "GroupName": "xxxxxx", "IPList": ["10.20.30.40", "50.60.70.80"]}'
    ```

    





