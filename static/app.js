// Modern WFM JavaScript

(function() {
  'use strict';

  var uploadReplaceConfirmCallback = null;

  function showUploadReplaceModal(fileNames, onConfirm) {
    var modal = document.getElementById('uploadReplaceModal');
    var listEl = document.getElementById('uploadReplaceList');
    var confirmBtn = document.getElementById('uploadReplaceConfirm');
    var cancelBtn = document.getElementById('uploadReplaceCancel');
    if (!modal || !listEl) return;
    listEl.innerHTML = '';
    fileNames.forEach(function(name) {
      var li = document.createElement('li');
      li.textContent = name;
      listEl.appendChild(li);
    });
    uploadReplaceConfirmCallback = onConfirm;
    modal.classList.add('visible');
    modal.setAttribute('aria-hidden', 'false');
  }

  function hideUploadReplaceModal() {
    var modal = document.getElementById('uploadReplaceModal');
    if (modal) {
      modal.classList.remove('visible');
      modal.setAttribute('aria-hidden', 'true');
      uploadReplaceConfirmCallback = null;
    }
  }

  function initUploadReplaceModal() {
    var modal = document.getElementById('uploadReplaceModal');
    var confirmBtn = document.getElementById('uploadReplaceConfirm');
    var cancelBtn = document.getElementById('uploadReplaceCancel');
    if (!modal || !confirmBtn || !cancelBtn) return;

    confirmBtn.addEventListener('click', function() {
      if (uploadReplaceConfirmCallback) uploadReplaceConfirmCallback();
      hideUploadReplaceModal();
    });
    cancelBtn.addEventListener('click', hideUploadReplaceModal);
    modal.addEventListener('click', function(e) {
      if (e.target === modal) hideUploadReplaceModal();
    });

    var form = document.getElementById('wfmForm');
    var fileInput = form && form.querySelector('input[name="filename"]');
    if (form && fileInput) {
      form.addEventListener('submit', function(e) {
        var sub = e.submitter;
        if (!sub || sub.name !== 'upload' || sub.value !== '1') return;
        var files = fileInput.files;
        if (!files || files.length === 0) return;
        var existing = window.wfmExistingFiles || [];
        var wouldReplace = [];
        for (var i = 0; i < files.length; i++) {
          if (existing.indexOf(files[i].name) !== -1) wouldReplace.push(files[i].name);
        }
        if (wouldReplace.length === 0) return;
        e.preventDefault();
        showUploadReplaceModal(wouldReplace, function() {
          form.submit();
        });
      });
    }
  }

  // Initialize drag and drop
  function initDragDrop() {
    const form = document.getElementById('wfmForm');
    if (!form) return;

    const dirInput = form.querySelector('input[name="dir"]');
    const sortInput = form.querySelector('input[name="sort"]');
    if (!dirInput || !sortInput) return;

    function prevent(e) {
      e.preventDefault();
      e.stopPropagation();
    }

    function onDragOver(e) {
      prevent(e);
      if (e.dataTransfer.types.indexOf('Files') >= 0) {
        document.body.classList.add('drag-over');
      }
    }

    function onDragLeave(e) {
      prevent(e);
      if (!e.relatedTarget || !document.body.contains(e.relatedTarget)) {
        document.body.classList.remove('drag-over');
      }
    }

    function doDropUpload(files) {
      const dir = dirInput.value;
      const sort = sortInput.value || '';
      const action = form.action;
      function upload(i) {
        if (i >= files.length) {
          if (i > 0) location.reload();
          return;
        }
        const fd = new FormData();
        fd.append('dir', dir);
        fd.append('sort', sort);
        fd.append('upload', '1');
        fd.append('filename', files[i]);
        fetch(action, {
          method: 'POST',
          body: fd,
          redirect: 'manual'
        }).then(function(r) {
          if (r.type === 'opaqueredirect' || (r.status >= 300 && r.status < 400)) {
            if (i + 1 >= files.length) location.reload();
            else upload(i + 1);
          } else {
            upload(i + 1);
          }
        }).catch(function() {
          upload(i + 1);
        });
      }
      upload(0);
    }

    function onDrop(e) {
      prevent(e);
      document.body.classList.remove('drag-over');
      const files = e.dataTransfer.files;
      if (!files || !files.length) return;

      const existing = window.wfmExistingFiles || [];
      const wouldReplace = [];
      for (let i = 0; i < files.length; i++) {
        if (existing.indexOf(files[i].name) !== -1) wouldReplace.push(files[i].name);
      }
      if (wouldReplace.length > 0) {
        showUploadReplaceModal(wouldReplace, function() {
          doDropUpload(files);
        });
        return;
      }
      doDropUpload(files);
    }

    document.addEventListener('dragover', onDragOver);
    document.addEventListener('dragenter', onDragOver);
    document.addEventListener('dragleave', onDragLeave);
    document.addEventListener('drop', onDrop);
  }

  // Initialize checkboxes for multi-select
  function initCheckboxes() {
    const checkboxes = document.querySelectorAll('input[name="mulf"]');
    const selectAllBtn = document.getElementById('selectAll');
    
    if (selectAllBtn) {
      selectAllBtn.addEventListener('click', function(e) {
        e.preventDefault();
        const allChecked = Array.from(checkboxes).every(cb => cb.checked);
        checkboxes.forEach(cb => {
          cb.checked = !allChecked;
        });
        updateSelectAllButton();
      });
    }

    checkboxes.forEach(cb => {
      cb.addEventListener('change', updateSelectAllButton);
    });

    function updateSelectAllButton() {
      if (selectAllBtn) {
        const allChecked = Array.from(checkboxes).every(cb => cb.checked);
        const someChecked = Array.from(checkboxes).some(cb => cb.checked);
        selectAllBtn.textContent = allChecked ? 'Deselect All' : 'Select All';
        selectAllBtn.style.display = someChecked || allChecked ? 'inline-block' : 'none';
      }
    }
  }

  // Initialize on DOM ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      initUploadReplaceModal();
      initDragDrop();
      initCheckboxes();
    });
  } else {
    initUploadReplaceModal();
    initDragDrop();
    initCheckboxes();
  }
})();
