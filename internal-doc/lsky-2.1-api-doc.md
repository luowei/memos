# Lsky Pro 2.1 API

官方文档参考：https://lsky-pro.apifox.cn/api-195745209

## 以下是我的 lsky 接口文档

接口说明

接口URL

https://lsky.wodedata.com/api/v1
验证方式

当前版本接口采用 「HTTP 基本验证」的方式验证授权，获取到 token 后，通过设置请求 header 标头来验证请求(Bearer Token)，例如：
"Authorization": "Bearer <lsky api token>"
如果未设置 Authorization 的情况下请求上传接口，将会被视为游客上传。

兰空图床(lsky)接口的 token 获取方式  
```
curl --location 'https://lsky.wodedata.com/api/v1/tokens' \
--header 'Content-Type: application/x-www-form-urlencoded' \
--data-urlencode 'email=<lsky login email>' \
--data-urlencode 'password=<password>'
```

---

公共请求 headers 说明

字段	类型	说明
Authorization	String	授权 Token，例如：Bearer <lsky api token>
*Accept	String	必须设置为 application/json
公共响应 headers 说明

字段	类型	说明
X-RateLimit-Limit	Integer	当前客户端一分钟内请求配额
X-RateLimit-Remaining	Integer	当前客户端剩余请求配额
响应状态码 HTTP Status Code 说明

状态码	说明
401	未登录或授权失败
403	管理员关闭了接口功能
429	超出请求配额，请求受限
500	服务端出现异常
文档中接口的请求参数，使用红色「*」符号标注，则表示为必传项。

授权相关

生成 Token

/tokens
请求参数(Body)

字段	类型	说明
*email	String	邮箱
*password	String	密码
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据
token	String	Token
清空 Token

/tokens
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据
用户资料

/profile
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据
name	String	用户名
avatar	String	头像地址
email	String	邮箱地址
capacity	Float	总容量
used_capacity	Float	已使用容量
url	String	个人主页地址
image_num	Integer	图片数量
album_num	Integer	相册数量
registered_ip	String	注册 IP
策略相关

策略列表

/strategies
请求参数(Query)

字段	类型	说明
keyword	String	筛选关键字
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据
strategies	Object[]	策略数据
id	Integer	策略 ID
name	String	策略名称
图片相关

上传图片

/upload
Headers

字段	类型	说明
*Content-Type	String	需要设置为 multipart/form-data
请求参数(Body)

字段	类型	说明
*file	File	图片文件
strategy_id	Integer	储存策略ID
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据
key	String	图片唯一密钥
name	String	图片名称
pathname	String	图片路径名
origin_name	String	图片原始名
size	Float	图片大小，单位 KB
mimetype	String	图片类型
extension	String	图片拓展名
md5	String	图片 md5 值
sha1	String	图片 sha1 值
links	Object	链接
url	String	图片访问 url
html	String	-
bbcode	String	-
markdown	String	-
markdown_with_link	String	-
thumbnail_url	String	缩略图 url
图片列表

/images
请求参数(Query)

字段	类型	说明
page	Integer	页码
order	String	排序方式，newest=最新，earliest=最早，utmost=最大，least=最小
permission	String	权限，public=公开的，private=私有的
album_id	Integer	相册 ID
keyword	String	筛选关键字
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据
current_page	Integer	当前所在页页码
last_page	Integer	最后一页页码
per_page	Integer	每页展示数据数量
total	Integer	图片总数量
data	Object[]	图片列表
key	String	图片唯一密钥
name	String	图片名称
origin_name	String	图片原始名称
pathname	String	图片路径名
size	Float	图片大小，单位 KB
width	Integer	图片宽度
height	Integer	图片高度
md5	String	图片 md5 值
sha1	String	图片 sha1 值
human_date	String	上传时间(友好格式)
date	String	上传日期(yyyy-MM-dd HH:mm:ss)
links	Object	链接，与上传接口返回参数中的 links 相同
删除图片

/images/:key
请求参数(Params)

字段	类型	说明
*key	String	图片密钥
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据
相册相关

相册列表

/albums
请求参数(Query)

字段	类型	说明
page	Integer	页码
order	String	排序方式，newest=最新，earliest=最早，most=图片最多，least=图片最少
keyword	String	筛选关键字
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据
current_page	Integer	当前所在页页码
last_page	Integer	最后一页页码
per_page	Integer	每页展示数据数量
total	Integer	图片总数量
data	Object[]	相册列表
id	Integer	相册自增 ID
name	String	相册名称
intro	String	相册简介
image_num	Integer	相册图片数量
删除相册

/albums/:id
请求参数(Params)

字段	类型	说明
*id	String	相册自增 ID
返回参数

字段	类型	说明
status	Boolean	状态，true 或 false
message	String	描述信息
data	Object	数据