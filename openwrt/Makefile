include $(TOPDIR)/rules.mk

PKG_NAME:=clash-exporter
PKG_VERSION:=0.2.0
PKG_RELEASE:=1

PKG_LICENSE:=MIT
PKG_LICENSE_FILES:=LICENSE
PKG_MAINTAINER:=Ghostpanter

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

GO_PKG:=github.com/Ghostpanter/clash-exporter
GO_PKG_LDFLAGS_X:=main.version=$(PKG_VERSION)

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/clash-exporter
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Prometheus exporter for OpenClash / Mihomo
  URL:=https://github.com/Ghostpanter/clash-exporter
  DEPENDS:=$(GO_ARCH_DEPENDS)
endef

define Package/clash-exporter/description
  Prometheus exporter for the OpenClash external controller (Mihomo).
  Reads cn_port and dashboard_password from OpenClash and exposes
  /metrics on port 2112.
endef

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) ./src/. $(PKG_BUILD_DIR)/
endef

define Package/clash-exporter/install
	$(call GoPackage/Package/Install/Bin,$(1))
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./files/etc/init.d/clash-exporter $(1)/etc/init.d/clash-exporter
	$(INSTALL_CONF) ./files/etc/config/clash-exporter $(1)/etc/config/clash-exporter
	$(INSTALL_BIN) ./files/etc/uci-defaults/99-clash-exporter $(1)/etc/uci-defaults/99-clash-exporter
endef

$(eval $(call GoBinPackage,clash-exporter))
$(eval $(call BuildPackage,clash-exporter))
