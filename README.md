# `hkbi`

**Easy native HomeKit-BlueIris integration**

`hkbi` allows you to natively integrate BlueIris into your home without
the effort of dealing with WebRTC, or attempting to configure generic
solutions to deal with BlueIris. With `hkbi` you simply need to pass a
config file containing your BlueIris credentials and set up a trigger
for motion alerts via HomeKit.

### Usage

```bash
$ hkbi ./config.toml
```

To pair, enter the pin `11111112`.

### Config

```toml
listen-address = "0.0.0.0:53238"
data-dir = "/var/lib/hkbi/"

[blueiris]
instance = "http://127.0.0.1:81"
username = "abcdef"
password = "123456"
```

### BlueIris Trigger Setup

Go to your camera's settings, select `Trigger` and enable `Motion Sensor`. Now go to the
`Alerts` tab, and create an `On alert...` HTTP request pointing to
`http://127.0.0.1:3333/trigger?state=on&cam=&CAM`. Do the same for `On reset...` but with
`state=off`. `&CAM` is a magic value in BlueIris referring to your camera's ID.

### Alternatives

There's a major open-source community around HomeKit, and security systems
in home automation - with various ways of integrating BlueIris into them.

- [hkcam](https://github.com/brutella/hkcam)
- [Scrypted](https://github.com/koush/scrypted)
- [ha-blueiris](https://github.com/elad-bar/ha-blueiris)
