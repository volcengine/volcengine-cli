## Volcengine Command Line Tools

[中文](README.MD) | English

### Special Note

To make it easier for users, starting from version v1.0.20, the command prefix for the Volcengine CLI tool has been changed from 'volcengine-cli' to 've'. Earlier versions are not affected. Users who have upgraded to v1.0.20 or later should promptly update the command prefix (refer to the alias command for unified configuration).

### Overview

1.  VolcEngine Command Line Tool
2.  The minimum required Go version is 1.5 or higher; version 1.12 or above is recommended.

### Install VolcEngine CLI

#### Obtain the client from the release package.

1.  <https://github.com/volcengine/volcengine-cli/releases> Get the latest version
2.  Download the version corresponding to your operating system and extract it for use.

#### Obtaining the client by compiling from source

1.  Compile the client using build.sh.
```shell
# If using a Mac 
sh build.sh darwin
mv ve-darwin ve
# If Using Windows
sh build.sh windows
mv ve-windows ve
# If using Linux
sh build.sh linux
mv ve-linux ve
```


### Configuring VolcEngine CLI

Add the Volcano Engine CLI to the environment variables.

1.  Check whether /usr/local/bin exists in the $PATH system variable. If it does not, please set a usable environment variable for the Volcano Engine CLI according to your actual situation.

2.  Execute the following command to copy 've' to the /usr/local/bin directory, after which it will be available for use.

    ```shell
    sudo cp ve /usr/local/bin
    ```

#### Configure Credentials

To invoke the service, you must provide AK, SK, and region. These parameters can be configured using either of the following two methods.

1.  Example of configuration using 've configure set'

    ```shell
    ve configure set --profile test --region cn-beijing --access-key ak --secret-key sk
    ```

    Supported Parameters and Related Descriptions

    ```shell
	profile: Configuration name. If the profile already exists, this command will modify the existing configuration; otherwise, it will create a new configuration and set the currently used profile to the new configuration. 
	access-key: Your Access Key 
	secret-key: Your SK 
	region: Geographic area, such as cn-beijing 
	session-token: Required if role-playing is used. 
	disable-ssl: Specifies whether to disable SSL. The default value is false. 
	endpoint: Optional; the default value is open.volcengineapi.com.
    ```

2.  If no configuration is specified in the configuration file, an attempt will be made to read the configuration from environment variables set via export.
    ```shell
	export VOLCENGINE_ACCESS_KEY=AK
	export VOLCENGINE_SECRET_KEY=SK 
	export VOLCENGINE_REGION=cn-bejing 

	# Whether to disable SSL; if not set, the default is false. 
	export VOLCENGINE_DISABLE_SSL=false 

	# If role-playing is employed, the following must be provided 
	export VOLCENGINE_SESSION_TOKEN=sessionToken
    ```

#### Other Operations of the configure Command

1.  After configuring a profile using the Volcengine CLI 'configure' command, the CLI will prioritize the authentication information within the profile for API request signing.
2.  After a profile is added or modified, the default profile used in the current configuration will be switched to the newly added or modified profile.
3.  If multiple profiles exist, use 've configure profile --profile [configuration name]' to switch before invoking the interface to ensure correct usage.

###### Retrieve specified profile configuration information

```shell
ve configure get --profile [profile name]
```

The 'profile' field is optional. If the 'profile' field is not specified, the current configuration will be displayed. If the 'profile' field is specified, the Volcano Engine CLI will attempt to retrieve and display the specified configuration.

If the configuration does not exist, all configuration fields in the returned result will be set to their default values.

###### Display information for all current configuration profiles.

```shell
ve configure list
```

###### Switch the currently used configuration (profile). Please ensure that the version is greater than or equal to 1.0.16.

```shell
ve configure profile --profile [profile name]
```

The 'profile' field is required. When a profile is specified, the Volcengine CLI will attempt to switch to the corresponding configuration.

If the configuration does not exist, the current configuration will not be switched, and an error message will be displayed.

###### Create/Modify Configuration Profile

```shell
ve configure set --profile [Profile Name] --region [Region] --access-key [User's AK] --secret-key [User's SK] --endpoint [Endpoint corresponding to the region]
```

After creating or modifying a configuration, the active configuration will switch to the newly created or modified configuration.

When creating a new configuration, the profile field and region field must be specified; when modifying an existing configuration, the profile field must be specified.

Other fields that can be specified:

-   access-key
-   secret-key
-   region
-   session-token
-   disable-ssl

For information on the function of each field, please refer to the 'Supported Parameters and Related Descriptions' section in the previous section.

###### Delete Configuration Profile

```shell
ve configure delete --profile [profile name]
```

When deleting a configuration, the profile field must be specified to indicate the name of the configuration to be deleted.

If the configuration to be deleted is currently in use, then after successful deletion, Volcano Engine CLI will attempt to randomly select one of the remaining configurations as the new current configuration.

#### Configure Auto-Completion

By executing ve completion --help, users can view the methods for configuring auto-completion in various terminals. Based on the instructions provided, users may choose whether to configure the auto-completion feature.

<br/>

##### Bash

The Bash completion script for Volcano Engine CLI can be viewed using 've completion bash'. To enable the autocompletion feature, import this script into your shell.

###### 1. Install bash-completion

The Volcengine CLI completion script depends on the bash-completion tool. Therefore, you must install and enable bash-completion in advance. You can use the command 'type _init_completion' to verify whether bash-completion is installed.

1.  Installation: yum install bash-completion or apt-get install bash-completion

2.  Enable bash-completion: source /usr/share/bash-completion/bash_completion

    It is recommended to add 'source /usr/share/bash-completion/bash_completion' to your ~/.bashrc file.

3.  Verify whether bash-completion is installed successfully: execute type _init_completion to check the installation status of bash-completion.

###### 2. Configuring Auto-Completion

Follow these steps to enable the auto-completion feature in Bash:

1.  echo 'source <(ve completion bash)' >> ~/.bashrc
2.  ve completion bash > /etc/bash_completion.d/ve

The changes will take effect after reloading the Shell or executing 'source ~/.bashrc'.

If the error '_get_comp_words_by_ref: command not found' occurs, please verify again that bash-completion has been installed and configured correctly.

<br/>

##### Zsh

The Zsh completion script for Volcengine CLI can be viewed by executing 've completion zsh'. To enable the auto-completion feature, import this completion script into your shell.

Configure the auto-completion feature in Zsh by following these steps:

1.  Enabling compinit in Zsh:
	echo "autoload -U compinit; compinit" >> ~/.zshrc
2.  Configure Auto-Completion
	ve completion zsh > "${fpath[1]}/_ve"

The changes will take effect after reloading the shell or executing 'source ~/.zshrc'.

#### Configure Color

The colored display feature can be enabled by using ve enable-color.

The ve disable-color command can be used to disable the color display feature.

### Using Volcengine CLI

#### Query the list of supported services and the list of supported interfaces under each service

1.  Query the list of supported services
    ```shell
    ve [-h|--help]
    ```
2.  List of Supported Interfaces under the Query Service
    ```shell
    ve ecs [-h|--help]
    ```

#### Call the API

When using the VolcEngine CLI to invoke an API, the basic command structure is as follows:

```shell
ve <service name> <action> [--parameter1 value1 --parameter2 value2 ...]
```

You can use ve <service name> <action> --help to view information such as the parameter list and return results for the API you wish to invoke. For example: ve ecs DescribeInstances --help.

For more detailed information, you may also refer to <https://www.volcengine.com/docs>.

<br/>

The following code examples are provided:

-   Basic Usage

    ```shell
    ve ecs DescribeInstances
    ```

    ```shell
    ve rds_mysql ListDBInstanceIPLists --InstanceId "xxxxxx"
    ```

-   Supports JSON input

    ```shell
    ve rds_mysql ModifyDBInstanceIPList --InstanceId "xxxxxx" --GroupName "xxxxxx" --IPList '["10.20.30.40", "50.60.70.80"]'
    ```

-   For requests with ContentType set to application/json, the Volcengine CLI also supports passing the body directly as JSON data.

    ```shell
    ve rds_mysql ModifyDBInstanceIPList --body '{"InstanceId":"xxxxxx", "GroupName": "xxxxxx", "IPList": ["10.20.30.40", "50.60.70.80"]}'
    ```
